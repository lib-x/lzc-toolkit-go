package manifest

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	lpkgo "github.com/lib-x/lzc-toolkit-go"
)

// BuildContext contains the values available to manifest build directives.
type BuildContext struct {
	Profile string
	Env     map[string]string
}

// IncludeFS is the filesystem contract used to load active build includes.
type IncludeFS = fs.FS

var (
	environmentKeyPattern   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	profileConditionPattern = regexp.MustCompile(`^profile\s*(==|=|!=)\s*(.+)$`)
	envConditionPattern     = regexp.MustCompile(`^env\.([A-Za-z_][A-Za-z0-9_]*)\s*(==|=|!=)\s*(.+)$`)
	envPresencePattern      = regexp.MustCompile(`^env\.([A-Za-z_][A-Za-z0-9_]*)$`)
)

type buildDirective struct {
	command string
	args    string
}

type buildFrame struct {
	parentActive bool
	condition    bool
	elseSeen     bool
	active       bool
	line         int
}

type preprocessOptions struct {
	includes    fs.FS
	includeRoot string
	displayRoot string
	context     context.Context
}

// Preprocess evaluates build directives in input. A live context is required
// and is checked throughout line processing and include reads.
func Preprocess(ctx context.Context, sourceName string, input []byte, buildContext BuildContext, includes fs.FS) ([]byte, error) {
	if sourceName == "" {
		sourceName = "<manifest>"
	}
	if err := preprocessContextError(ctx, "manifest.preprocess", sourceName); err != nil {
		return nil, err
	}
	buildContext.Profile = strings.TrimSpace(buildContext.Profile)
	buildContext.Env = cloneEnvironment(buildContext.Env)
	for key := range buildContext.Env {
		if err := preprocessContextError(ctx, "manifest.preprocess", sourceName); err != nil {
			return nil, err
		}
		if !environmentKeyPattern.MatchString(key) {
			return nil, directiveError(sourceName, 0, "manifest.preprocess.environment")
		}
	}
	options := preprocessOptions{
		includes:    includes,
		includeRoot: path.Dir(sourceName),
		displayRoot: path.Dir(sourceName),
		context:     ctx,
	}
	return processManifestText(sourceName, string(input), buildContext, options, true)
}

// PreprocessFile resolves filename, confines source and include reads to one
// rooted directory, and checks cancellation throughout preprocessing.
func PreprocessFile(ctx context.Context, filename string, buildContext BuildContext) (output []byte, resultErr error) {
	if err := preprocessContextError(ctx, "manifest.preprocess_file", filename); err != nil {
		return nil, err
	}
	resolvedFilename, err := filepath.Abs(filename)
	if err != nil {
		return nil, &lpkgo.Error{Code: lpkgo.CodeInvalidManifest, Op: "manifest.preprocess_file", Path: filepath.ToSlash(filename), Cause: errors.New("manifest source path resolution failed")}
	}
	resolvedFilename = filepath.Clean(resolvedFilename)
	sourceName := filepath.ToSlash(resolvedFilename)
	if err := preprocessContextError(ctx, "manifest.preprocess_file", sourceName); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(filepath.Dir(resolvedFilename))
	if err != nil {
		return nil, &lpkgo.Error{Code: lpkgo.CodeInvalidManifest, Op: "manifest.preprocess_file", Path: sourceName, Cause: errors.New("manifest source root open failed")}
	}
	defer func() {
		if err := root.Close(); err != nil && resultErr == nil {
			output = nil
			resultErr = &lpkgo.Error{Code: lpkgo.CodeInvalidManifest, Op: "manifest.preprocess_file.close", Path: sourceName, Cause: errors.New("manifest source root close failed")}
		}
	}()
	input, err := root.ReadFile(filepath.Base(resolvedFilename))
	if err != nil {
		return nil, &lpkgo.Error{Code: lpkgo.CodeInvalidManifest, Op: "manifest.preprocess_file", Path: sourceName, Cause: errors.New("manifest source read failed")}
	}
	if err := preprocessContextError(ctx, "manifest.preprocess_file", sourceName); err != nil {
		return nil, err
	}
	buildContext.Profile = strings.TrimSpace(buildContext.Profile)
	buildContext.Env = cloneEnvironment(buildContext.Env)
	for key := range buildContext.Env {
		if err := preprocessContextError(ctx, "manifest.preprocess_file", sourceName); err != nil {
			return nil, err
		}
		if !environmentKeyPattern.MatchString(key) {
			return nil, directiveError(sourceName, 0, "manifest.preprocess.environment")
		}
	}
	return processManifestText(sourceName, string(input), buildContext, preprocessOptions{
		includes:    root.FS(),
		includeRoot: ".",
		displayRoot: filepath.ToSlash(filepath.Dir(resolvedFilename)),
		context:     ctx,
	}, true)
}

func processManifestText(sourceName string, input string, context BuildContext, options preprocessOptions, allowDirectives bool) ([]byte, error) {
	lines := strings.Split(input, "\n")
	output := make([]string, 0, len(lines))
	stack := []buildFrame{{active: true}}
	currentActive := func() bool { return stack[len(stack)-1].active }

	for index, line := range lines {
		if err := preprocessContextError(options.context, "manifest.preprocess", sourceName); err != nil {
			return nil, err
		}
		directive, found, parseErr := parseBuildDirective(line)
		if parseErr != nil {
			return nil, directiveError(sourceName, index+1, "manifest.preprocess.directive")
		}
		if !found {
			if currentActive() {
				output = append(output, line)
			}
			continue
		}
		if !allowDirectives {
			return nil, directiveError(sourceName, index+1, "manifest.preprocess.include.directive")
		}

		switch directive.command {
		case "if":
			condition, err := evaluateBuildCondition(directive.args, context)
			if err != nil {
				return nil, directiveError(sourceName, index+1, "manifest.preprocess.condition")
			}
			parentActive := currentActive()
			stack = append(stack, buildFrame{
				parentActive: parentActive,
				condition:    condition,
				active:       parentActive && condition,
				line:         index + 1,
			})
		case "else":
			if len(stack) == 1 {
				return nil, directiveError(sourceName, index+1, "manifest.preprocess.else")
			}
			top := &stack[len(stack)-1]
			if top.elseSeen {
				return nil, directiveError(sourceName, index+1, "manifest.preprocess.else")
			}
			top.elseSeen = true
			top.active = top.parentActive && !top.condition
		case "end":
			if len(stack) == 1 {
				return nil, directiveError(sourceName, index+1, "manifest.preprocess.end")
			}
			stack = stack[:len(stack)-1]
		case "include":
			if !currentActive() {
				continue
			}
			includeName, displayName, ok := resolveIncludeName(options, directive.args)
			if !ok || options.includes == nil {
				return nil, directiveError(sourceName, index+1, "manifest.preprocess.include")
			}
			if options.context != nil {
				if err := preprocessContextError(options.context, "manifest.preprocess", displayName); err != nil {
					return nil, err
				}
			}
			included, err := fs.ReadFile(options.includes, includeName)
			if err != nil {
				return nil, directiveError(sourceName, index+1, "manifest.preprocess.include")
			}
			if options.context != nil {
				if err := preprocessContextError(options.context, "manifest.preprocess", displayName); err != nil {
					return nil, err
				}
			}
			rendered, err := processManifestText(displayName, string(included), context, options, false)
			if err != nil {
				return nil, err
			}
			if len(rendered) != 0 {
				output = append(output, indentIncludedText(string(rendered), leadingIndent(line)))
			}
		default:
			return nil, directiveError(sourceName, index+1, "manifest.preprocess.directive")
		}
	}
	if len(stack) != 1 {
		return nil, directiveError(sourceName, stack[len(stack)-1].line, "manifest.preprocess.if")
	}
	return []byte(strings.Join(output, "\n")), nil
}

func parseBuildDirective(line string) (buildDirective, bool, error) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#@build") {
		return buildDirective{}, false, nil
	}
	body := strings.TrimSpace(strings.TrimPrefix(trimmed, "#@build"))
	if body == "" {
		return buildDirective{}, true, errors.New("empty build directive")
	}
	space := strings.IndexByte(body, ' ')
	if space < 0 {
		return buildDirective{command: body}, true, nil
	}
	return buildDirective{
		command: strings.TrimSpace(body[:space]),
		args:    strings.TrimSpace(body[space+1:]),
	}, true, nil
}

func resolveIncludeName(options preprocessOptions, rawName string) (string, string, bool) {
	name := normalizeDirectiveValue(rawName)
	if name == "" || path.IsAbs(name) || strings.Contains(name, `\`) {
		return "", "", false
	}
	name = path.Clean(path.Join(options.includeRoot, name))
	if name == "." || name == ".." || strings.HasPrefix(name, "../") || !fs.ValidPath(name) {
		return "", "", false
	}
	displayName := path.Clean(path.Join(options.displayRoot, normalizeDirectiveValue(rawName)))
	return name, displayName, true
}

func leadingIndent(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}

func indentIncludedText(text string, indent string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	for index, line := range lines {
		if line != "" {
			lines[index] = indent + line
		}
	}
	return strings.Join(lines, "\n")
}

func evaluateBuildCondition(expression string, context BuildContext) (bool, error) {
	if match := profileConditionPattern.FindStringSubmatch(strings.TrimSpace(expression)); match != nil {
		return compareDirectiveValues(context.Profile, normalizeDirectiveValue(match[2]), match[1]), nil
	}
	if match := envConditionPattern.FindStringSubmatch(strings.TrimSpace(expression)); match != nil {
		return compareDirectiveValues(context.Env[match[1]], normalizeDirectiveValue(match[3]), match[2]), nil
	}
	if match := envPresencePattern.FindStringSubmatch(strings.TrimSpace(expression)); match != nil {
		return strings.TrimSpace(context.Env[match[1]]) != "", nil
	}
	return false, errors.New("unsupported build condition")
}

func compareDirectiveValues(actual string, expected string, operator string) bool {
	if operator == "!=" {
		return actual != expected
	}
	return actual == expected
}

func normalizeDirectiveValue(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if value[0] == value[len(value)-1] && (value[0] == '\'' || value[0] == '"') {
			return value[1 : len(value)-1]
		}
	}
	return value
}

func cloneEnvironment(environment map[string]string) map[string]string {
	clone := make(map[string]string, len(environment))
	for key, value := range environment {
		clone[key] = value
	}
	return clone
}

func directiveError(sourceName string, line int, op string) error {
	return &lpkgo.Error{
		Code:  lpkgo.CodeInvalidManifest,
		Op:    op,
		Path:  sourceReference(sourceName, line),
		Cause: errors.New("invalid build directive"),
	}
}

func sourceReference(sourceName string, line int) string {
	return sourceName + ":" + strconv.Itoa(line)
}

func preprocessContextError(ctx context.Context, op string, filename string) error {
	if ctx == nil {
		return &lpkgo.Error{Code: lpkgo.CodeInvalidArgument, Op: op, Path: filepath.ToSlash(filename), Cause: errors.New("nil context")}
	}
	if err := ctx.Err(); err != nil {
		return &lpkgo.Error{Code: lpkgo.CodeCancelled, Op: op, Path: filepath.ToSlash(filename), Cause: err}
	}
	return nil
}
