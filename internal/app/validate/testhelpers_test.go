package validate_test

import "strings"

// containsStr is a small helper used by all check tests to assert that a
// message contains an expected fragment.
func containsStr(s, substr string) bool {
	return strings.Contains(s, substr)
}
