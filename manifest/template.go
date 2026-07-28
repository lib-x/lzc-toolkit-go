package manifest

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
	"go.yaml.in/yaml/v3"
)

const (
	templateControlPrefix    = "lzc-toolkit-template-control-"
	templateExpressionPrefix = "lzc-toolkit-template-expression-"
)

// TemplateInfo summarizes template actions without exposing their bodies.
type TemplateInfo struct {
	Present              bool     `json:"present"`
	ControlCount         int      `json:"controlCount"`
	ExpressionCount      int      `json:"expressionCount"`
	HasConditionalBlocks bool     `json:"hasConditionalBlocks"`
	HasInlineConditions  bool     `json:"hasInlineConditions"`
	ActionKinds          []string `json:"actionKinds"`
}

// Analysis holds a non-executing projection of a possibly templated manifest.
type Analysis struct {
	document  *Document
	template  TemplateInfo
	markers   []templateMarker
	lineDepth map[int]int
}

type templateMarker struct {
	token   []byte
	action  []byte
	control bool
	line    int
	depth   int
	kind    string
}

type templateFrame struct {
	elseSeen bool
	line     int
}

// Analyze projects template actions into YAML-safe markers without parsing or
// executing their pipelines.
func Analyze(data []byte) (*Analysis, error) {
	if bytes.Contains(data, []byte(templateControlPrefix)) || bytes.Contains(data, []byte(templateExpressionPrefix)) {
		return nil, templateAnalysisError("manifest.analyze.marker", 0, "reserved template marker prefix")
	}

	actions, err := scanTemplateActions(data)
	if err != nil {
		return nil, err
	}
	var wholeLineControls map[int]bool
	if len(actions) != 0 {
		wholeLineControls = wholeLineControlActions(data, actions)
	}
	analysis := &Analysis{lineDepth: make(map[int]int)}
	projected := append([]byte(nil), data...)
	stack := make([]templateFrame, 0)
	kinds := make(map[string]struct{})
	offset := 0

	for actionIndex, action := range actions {
		standalone := actionIsStandalone(data, action.start, action.end)
		actionBytes := data[action.start:action.end]
		kind := actionKind(actionBytes)
		promotedControl := wholeLineControls[actionIndex]
		control := (standalone || promotedControl) && isControlKind(kind)
		depth := len(stack)

		switch kind {
		case "if", "with", "range":
			stack = append(stack, templateFrame{line: action.line})
			if standalone {
				analysis.template.HasConditionalBlocks = true
			} else {
				analysis.template.HasInlineConditions = true
			}
		case "else":
			if len(stack) == 0 || stack[len(stack)-1].elseSeen {
				return nil, templateAnalysisError("manifest.analyze.control", action.line, "invalid template control structure")
			}
			if !isChainedElse(actionBytes) {
				stack[len(stack)-1].elseSeen = true
			}
			if !standalone {
				analysis.template.HasInlineConditions = true
			}
		case "end":
			if len(stack) == 0 {
				return nil, templateAnalysisError("manifest.analyze.control", action.line, "invalid template control structure")
			}
			stack = stack[:len(stack)-1]
			if !standalone {
				analysis.template.HasInlineConditions = true
			}
		}

		var token string
		if control {
			token = fmt.Sprintf("%s%d", templateControlPrefix, analysis.template.ControlCount)
			analysis.template.ControlCount++
		} else {
			token = fmt.Sprintf("%s%d", templateExpressionPrefix, analysis.template.ExpressionCount)
			analysis.template.ExpressionCount++
		}
		if _, exists := kinds[kind]; !exists {
			kinds[kind] = struct{}{}
			analysis.template.ActionKinds = append(analysis.template.ActionKinds, kind)
		}

		replacement := []byte(token)
		if control {
			replacement = []byte("# " + token)
			if promotedControl {
				indentation := lineIndentation(data, action.start)
				switch kind {
				case "if", "with", "range":
					replacement = append(replacement, append([]byte{'\n'}, indentation...)...)
				case "end":
					replacement = append(append([]byte{'\n'}, indentation...), replacement...)
				}
			}
		}
		start := action.start + offset
		end := action.end + offset
		projected = append(projected[:start], append(replacement, projected[end:]...)...)
		offset += len(replacement) - (action.end - action.start)
		analysis.markers = append(analysis.markers, templateMarker{
			token: []byte(token), action: append([]byte(nil), data[action.start:action.end]...),
			control: control, line: action.line, depth: depth, kind: kind,
		})
	}

	if len(stack) != 0 {
		return nil, templateAnalysisError("manifest.analyze.control", stack[len(stack)-1].line, "unclosed template control block")
	}
	sort.Strings(analysis.template.ActionKinds)
	analysis.lineDepth = projectedLineDepth(projected, analysis.markers)
	document, err := Parse(projected)
	if err != nil {
		return nil, err
	}
	analysis.document = document
	analysis.template.Present = len(actions) != 0
	return analysis, nil
}

func wholeLineControlActions(data []byte, actions []scannedTemplateAction) map[int]bool {
	promoted := make(map[int]bool)
	blockScalarLines := blockScalarContentLines(data)
	stack := make([]int, 0)
	for index, action := range actions {
		kind := actionKind(data[action.start:action.end])
		switch kind {
		case "if", "with", "range":
			stack = append(stack, index)
		case "end":
			if len(stack) == 0 {
				continue
			}
			openingIndex := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			opening := actions[openingIndex]
			if opening.line != action.line || blockScalarLines[opening.line] || !actionStartsPhysicalLine(data, opening.start) || !actionEndsPhysicalLine(data, action.end) {
				continue
			}
			if hasNestedControlAction(data, actions[openingIndex+1:index]) {
				continue
			}
			promoted[openingIndex] = true
			promoted[index] = true
		}
	}
	return promoted
}

func blockScalarContentLines(data []byte) map[int]bool {
	result := make(map[int]bool)
	headerIndent := -1
	for index, line := range bytes.Split(data, []byte{'\n'}) {
		trimmed := bytes.TrimSpace(line)
		indent := len(line) - len(bytes.TrimLeft(line, " \t"))
		if headerIndent >= 0 {
			if len(trimmed) == 0 {
				continue
			}
			if indent > headerIndent {
				result[index+1] = true
				continue
			}
			headerIndent = -1
		}
		if isBlockScalarHeader(line) {
			headerIndent = indent
		}
	}
	return result
}

func isBlockScalarHeader(line []byte) bool {
	content := strings.TrimSpace(stripYAMLLineComment(string(line)))
	fields := strings.Fields(content)
	if len(fields) < 2 {
		return false
	}
	indicator := fields[len(fields)-1]
	if !validBlockScalarIndicator(indicator) {
		return false
	}
	headerFields := fields[:len(fields)-1]
	for len(headerFields) > 0 {
		property := headerFields[len(headerFields)-1]
		if !strings.HasPrefix(property, "&") && !strings.HasPrefix(property, "!") {
			break
		}
		headerFields = headerFields[:len(headerFields)-1]
	}
	if len(headerFields) == 0 {
		return false
	}
	prefix := strings.Join(headerFields, " ")
	return prefix == "-" || strings.HasSuffix(prefix, ":")
}

func validBlockScalarIndicator(value string) bool {
	if len(value) < 1 || len(value) > 3 || (value[0] != '|' && value[0] != '>') {
		return false
	}
	seenChomp, seenIndent := false, false
	for _, character := range value[1:] {
		switch {
		case character == '+' || character == '-':
			if seenChomp {
				return false
			}
			seenChomp = true
		case character >= '1' && character <= '9':
			if seenIndent {
				return false
			}
			seenIndent = true
		default:
			return false
		}
	}
	return true
}

func stripYAMLLineComment(line string) string {
	inSingle, inDouble, escaped := false, false, false
	for index, character := range line {
		if inDouble && escaped {
			escaped = false
			continue
		}
		if inDouble && character == '\\' {
			escaped = true
			continue
		}
		switch character {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return line[:index]
			}
		}
	}
	return line
}

func hasNestedControlAction(data []byte, actions []scannedTemplateAction) bool {
	for _, action := range actions {
		if isControlKind(actionKind(data[action.start:action.end])) {
			return true
		}
	}
	return false
}

func actionStartsPhysicalLine(data []byte, start int) bool {
	lineStart := bytes.LastIndexByte(data[:start], '\n') + 1
	return len(bytes.TrimSpace(data[lineStart:start])) == 0
}

func actionEndsPhysicalLine(data []byte, end int) bool {
	lineEndOffset := bytes.IndexByte(data[end:], '\n')
	lineEnd := len(data)
	if lineEndOffset >= 0 {
		lineEnd = end + lineEndOffset
	}
	return len(bytes.TrimSpace(data[end:lineEnd])) == 0
}

func lineIndentation(data []byte, offset int) []byte {
	lineStart := bytes.LastIndexByte(data[:offset], '\n') + 1
	line := data[lineStart:offset]
	indentationLength := len(line) - len(bytes.TrimLeft(line, " \t"))
	return append([]byte(nil), line[:indentationLength]...)
}

func projectedLineDepth(projected []byte, markers []templateMarker) map[int]int {
	controls := make(map[string]templateMarker)
	for _, marker := range markers {
		if marker.control {
			controls[string(marker.token)] = marker
		}
	}
	depths := make(map[int]int)
	depth := 0
	for index, line := range bytes.Split(projected, []byte{'\n'}) {
		depths[index+1] = depth
		trimmed := strings.TrimSpace(string(line))
		if !strings.HasPrefix(trimmed, "# ") {
			continue
		}
		marker, found := controls[strings.TrimPrefix(trimmed, "# ")]
		if !found {
			continue
		}
		switch marker.kind {
		case "if", "with", "range":
			depth++
		case "end":
			if depth > 0 {
				depth--
			}
		}
	}
	return depths
}

// Document returns an independent copy of the projected YAML document.
func (analysis *Analysis) Document() *Document {
	if analysis == nil {
		return nil
	}
	return analysis.document.Clone()
}

// StaticScalar returns the decoded scalar at path when it has exactly one
// unconditional, expression-free occurrence in the analyzed manifest.
func (analysis *Analysis) StaticScalar(path ...string) (value any, found bool, err error) {
	if analysis == nil || analysis.document == nil || analysis.document.root == nil || len(path) == 0 {
		return nil, false, nil
	}
	current := documentContent(analysis.document.root)
	for depth, component := range path {
		if component == "" || current == nil || current.Kind != yaml.MappingNode {
			return nil, false, nil
		}
		var key, next *yaml.Node
		matches := 0
		for _, pair := range allMappingPairs(current) {
			if pair.key.Kind == yaml.ScalarNode && pair.key.Value == component {
				key, next = pair.key, pair.value
				matches++
			}
		}
		if matches != 1 || analysis.lineDepth[key.Line] > 0 || analysis.lineDepth[next.Line] > 0 {
			return nil, false, nil
		}
		if depth != len(path)-1 {
			current = next
			continue
		}
		resolved, ok := resolveAliasNode(next)
		if !ok || resolved.Kind != yaml.ScalarNode || analysis.lineDepth[resolved.Line] > 0 || nodeHasExpressionMarker(next) || nodeHasExpressionMarker(resolved) {
			return nil, false, nil
		}
		if err := next.Decode(&value); err != nil {
			return nil, false, manifestYAMLError("manifest.static_scalar", "decode", err)
		}
		return value, true, nil
	}
	return nil, false, nil
}

// Template returns an independent copy of the template summary.
func (analysis *Analysis) Template() TemplateInfo {
	if analysis == nil {
		return TemplateInfo{}
	}
	info := analysis.template
	info.ActionKinds = append([]string(nil), analysis.template.ActionKinds...)
	return info
}

func nodeHasExpressionMarker(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	if node.Kind == yaml.ScalarNode && strings.Contains(node.Value, templateExpressionPrefix) {
		return true
	}
	for _, child := range node.Content {
		if nodeHasExpressionMarker(child) {
			return true
		}
	}
	return false
}

// Restore replaces every projected marker with its exact original action.
func (analysis *Analysis) Restore(encoded []byte) ([]byte, error) {
	if analysis == nil || analysis.document == nil {
		return nil, templateAnalysisError("manifest.restore", 0, "nil template analysis")
	}
	restored := append([]byte(nil), encoded...)
	for _, marker := range analysis.markers {
		if _, count := exactMarkerOffset(restored, marker.token); count != 1 {
			return nil, templateAnalysisError("manifest.restore", marker.line, "template marker count mismatch")
		}
	}
	for _, marker := range analysis.markers {
		offset, _ := exactMarkerOffset(restored, marker.token)
		start, end := offset, offset+len(marker.token)
		if marker.control {
			lineStart := bytes.LastIndexByte(restored[:offset], '\n') + 1
			lineEndOffset := bytes.IndexByte(restored[end:], '\n')
			lineEnd := len(restored)
			if lineEndOffset >= 0 {
				lineEnd = end + lineEndOffset
			}
			expected := append([]byte("# "), marker.token...)
			if !bytes.Equal(bytes.TrimSpace(restored[lineStart:lineEnd]), expected) {
				return nil, templateAnalysisError("manifest.restore", marker.line, "invalid template control marker line")
			}
			start = lineStart + bytes.Index(restored[lineStart:offset], []byte("#"))
		}
		replacement := append([]byte(nil), marker.action...)
		restored = append(restored[:start], append(replacement, restored[end:]...)...)
	}
	return restored, nil
}

func exactMarkerOffset(data []byte, token []byte) (int, int) {
	first := -1
	count := 0
	for searchStart := 0; searchStart < len(data); {
		relative := bytes.Index(data[searchStart:], token)
		if relative < 0 {
			break
		}
		offset := searchStart + relative
		end := offset + len(token)
		if end == len(data) || data[end] < '0' || data[end] > '9' {
			if first < 0 {
				first = offset
			}
			count++
		}
		searchStart = offset + 1
	}
	return first, count
}

type scannedTemplateAction struct {
	start int
	end   int
	line  int
}

func scanTemplateActions(data []byte) ([]scannedTemplateAction, error) {
	actions := make([]scannedTemplateAction, 0)
	line := 1
	for index := 0; index < len(data); {
		if data[index] == '\n' {
			line++
			index++
			continue
		}
		if index+1 >= len(data) || data[index] != '{' || data[index+1] != '{' {
			index++
			continue
		}
		start, actionLine := index, line
		index += 2
		state := byte(0)
		escaped := false
		closed := false
		for index < len(data) {
			current := data[index]
			if current == '\n' {
				line++
			}
			if escaped {
				escaped = false
				index++
				continue
			}
			switch state {
			case 0:
				if current == '"' || current == '\'' || current == '`' {
					state = current
					index++
					continue
				}
				if current == '}' && index+1 < len(data) && data[index+1] == '}' {
					index += 2
					actions = append(actions, scannedTemplateAction{start: start, end: index, line: actionLine})
					closed = true
				}
			case '"', '\'':
				if current == '\\' {
					escaped = true
				} else if current == state {
					state = 0
				}
			case '`':
				if current == '`' {
					state = 0
				}
			}
			if closed {
				break
			}
			index++
		}
		if !closed {
			return nil, templateAnalysisError("manifest.analyze.scan", actionLine, "unclosed template action")
		}
	}
	return actions, nil
}

func actionIsStandalone(data []byte, start int, end int) bool {
	lineStart := bytes.LastIndexByte(data[:start], '\n') + 1
	lineEndOffset := bytes.IndexByte(data[end:], '\n')
	lineEnd := len(data)
	if lineEndOffset >= 0 {
		lineEnd = end + lineEndOffset
	}
	return len(bytes.TrimSpace(data[lineStart:start])) == 0 && len(bytes.TrimSpace(data[end:lineEnd])) == 0
}

func actionKind(action []byte) string {
	if fields := templateActionFields(action); len(fields) != 0 && safeActionKind(fields[0]) {
		return fields[0]
	}
	return "expression"
}

func safeActionKind(kind string) bool {
	if kind == "" || (!asciiLetter(kind[0]) && kind[0] != '_') {
		return false
	}
	for index := 1; index < len(kind); index++ {
		if asciiLetter(kind[index]) || kind[index] == '_' || (kind[index] >= '0' && kind[index] <= '9') {
			continue
		}
		return false
	}
	return true
}

func asciiLetter(value byte) bool {
	return value >= 'A' && value <= 'Z' || value >= 'a' && value <= 'z'
}

func isChainedElse(action []byte) bool {
	fields := templateActionFields(action)
	return len(fields) > 1 && (fields[1] == "if" || fields[1] == "with")
}

func templateActionFields(action []byte) []string {
	body := strings.TrimSpace(string(action[2 : len(action)-2]))
	body = strings.TrimSpace(strings.TrimPrefix(body, "-"))
	body = strings.TrimSpace(strings.TrimSuffix(body, "-"))
	return strings.Fields(body)
}

func isControlKind(kind string) bool {
	switch kind {
	case "if", "else", "end", "with", "range":
		return true
	default:
		return false
	}
}

func templateAnalysisError(op string, _ int, message string) error {
	return &lpkgo.Error{Code: lpkgo.CodeInvalidManifest, Op: op, Cause: errors.New(message)}
}
