package update

import (
	"strconv"
	"strings"
)

// NormalizeVersion strips surrounding whitespace and a leading "v" prefix.
func NormalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// ParseSemverCore parses MAJOR.MINOR.PATCH[-prerelease] into its numeric core,
// whether a prerelease suffix is present, and whether parsing succeeded.
func ParseSemverCore(v string) ([3]int, bool, bool) {
	v = NormalizeVersion(v)
	if v == "" || v == "dev" {
		return [3]int{}, false, false
	}
	core, _, hasPre := strings.Cut(v, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return [3]int{}, false, false
	}
	var nums [3]int
	for i, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 {
			return [3]int{}, false, false
		}
		nums[i] = n
	}
	return nums, hasPre, true
}

// IsValidSemver reports whether v has a valid MAJOR.MINOR.PATCH core.
func IsValidSemver(v string) bool {
	_, _, ok := ParseSemverCore(v)
	return ok
}

// IsNewerSemver reports whether latest is strictly newer than current.
func IsNewerSemver(current, latest string) bool {
	c, cPre, cOK := ParseSemverCore(current)
	l, lPre, lOK := ParseSemverCore(latest)
	if !cOK || !lOK {
		return false
	}
	for i := range 3 {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return cPre && !lPre
}
