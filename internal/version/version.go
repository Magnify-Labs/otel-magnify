// Package version provides shared semantic version comparison helpers.
package version

import (
	"strconv"
	"strings"
)

type semver struct {
	major int
	minor int
	patch int
	pre   []string
}

// Compare compares two semantic versions and returns -1 when a < b, 0 when
// a == b, and 1 when a > b. A leading "v" is accepted. Empty or invalid
// versions return ok=false so callers can treat them as unknown.
func Compare(a, b string) (cmp int, ok bool) {
	av, ok := parse(a)
	if !ok {
		return 0, false
	}
	bv, ok := parse(b)
	if !ok {
		return 0, false
	}
	if av.major != bv.major {
		return sign(av.major - bv.major), true
	}
	if av.minor != bv.minor {
		return sign(av.minor - bv.minor), true
	}
	if av.patch != bv.patch {
		return sign(av.patch - bv.patch), true
	}
	return comparePrerelease(av.pre, bv.pre), true
}

func parse(s string) (semver, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return semver{}, false
	}
	s = strings.TrimPrefix(s, "v")
	if plus := strings.IndexByte(s, '+'); plus >= 0 {
		s = s[:plus]
	}
	pre := ""
	if dash := strings.IndexByte(s, '-'); dash >= 0 {
		pre = s[dash+1:]
		s = s[:dash]
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	major, ok := parseNumber(parts[0])
	if !ok {
		return semver{}, false
	}
	minor, ok := parseNumber(parts[1])
	if !ok {
		return semver{}, false
	}
	patch, ok := parseNumber(parts[2])
	if !ok {
		return semver{}, false
	}
	var preParts []string
	if pre != "" {
		preParts = strings.Split(pre, ".")
		for _, p := range preParts {
			if p == "" {
				return semver{}, false
			}
		}
	}
	return semver{major: major, minor: minor, patch: patch, pre: preParts}, true
}

func parseNumber(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

func comparePrerelease(a, b []string) int {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	if len(a) == 0 {
		return 1
	}
	if len(b) == 0 {
		return -1
	}
	limit := len(a)
	if len(b) < limit {
		limit = len(b)
	}
	for i := 0; i < limit; i++ {
		ai, aNum := numericIdentifier(a[i])
		bi, bNum := numericIdentifier(b[i])
		switch {
		case aNum && bNum && ai != bi:
			return sign(ai - bi)
		case aNum && !bNum:
			return -1
		case !aNum && bNum:
			return 1
		case !aNum && !bNum && a[i] != b[i]:
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return sign(len(a) - len(b))
}

func numericIdentifier(s string) (int, bool) {
	return parseNumber(s)
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}
