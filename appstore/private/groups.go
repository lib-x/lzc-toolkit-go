package private

import "strings"

type GroupCodePlacement uint8

const (
	GroupCodesHeader GroupCodePlacement = iota
	GroupCodesQuery
	GroupCodesHeaderAndQuery
)

func validGroupCodePlacement(placement GroupCodePlacement) bool {
	return placement == GroupCodesHeader || placement == GroupCodesQuery || placement == GroupCodesHeaderAndQuery
}

func normalizeGroupCodes(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		code := strings.ToUpper(strings.TrimSpace(value))
		if !validGroupCode(code) {
			continue
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		result = append(result, code)
	}
	return result
}

func validGroupCode(code string) bool {
	if len(code) != 6 {
		return false
	}
	for _, char := range code {
		if (char < 'A' || char > 'Z') && (char < '0' || char > '9') {
			return false
		}
	}
	return true
}

func mergeGroupCodes(defaults, request []string) []string {
	values := make([]string, 0, len(defaults)+len(request))
	values = append(values, defaults...)
	values = append(values, request...)
	return normalizeGroupCodes(values)
}
