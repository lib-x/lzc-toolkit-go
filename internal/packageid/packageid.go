package packageid

import "regexp"

var pattern = regexp.MustCompile(`^[a-z][a-z0-9]*(-[a-z0-9]+)*(\.[a-z][a-z0-9]*(-[a-z0-9]+)*)*$`)

// Valid reports whether value is a valid LazyCat package identifier.
func Valid(value string) bool {
	return pattern.MatchString(value)
}
