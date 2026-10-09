package release

import (
	"strconv"
	"strings"
)

// Newer reports whether version latest is later than current, comparing their numeric parts
// (a leading v and any -prerelease or +build suffix are ignored); a release replaces a commit build.
func Newer(latest, current string) bool {
	a, okA := numbers(latest)
	if okA && commitBuild(current) {
		return true
	}
	b, okB := numbers(current)
	if !okA || !okB {
		return false
	}
	for i := range max(len(a), len(b)) {
		switch x, y := part(a, i), part(b, i); {
		case x > y:
			return true
		case x < y:
			return false
		}
	}
	return false
}

func commitBuild(version string) bool {
	core := strings.TrimSuffix(strings.TrimSpace(version), "-dirty")
	if len(core) < 7 || len(core) > 40 {
		return false
	}
	for _, c := range core {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func part(nums []int, i int) int {
	if i < len(nums) {
		return nums[i]
	}
	return 0
}

func numbers(version string) ([]int, bool) {
	core := strings.TrimPrefix(strings.TrimSpace(version), "v")
	core, _, _ = strings.Cut(core, "-")
	core, _, _ = strings.Cut(core, "+")
	var nums []int
	for _, field := range strings.Split(core, ".") {
		n, err := strconv.Atoi(field)
		if err != nil || n < 0 {
			return nil, false
		}
		nums = append(nums, n)
	}
	return nums, true
}
