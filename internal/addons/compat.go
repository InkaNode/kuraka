// Package addons defines the upstream versions tested with Kuraka's addons.
package addons

import (
	"regexp"
	"strconv"
	"strings"
)

var Supported = map[string]string{"stalwart": "1.0", "webui": "2.0", "libredesk": "2.9"}

var tagVersion = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)

// Allows compares major and minor numerically; patches and prereleases do not
// change compatibility. Unknown components have no compatibility restriction.
func Allows(component, tag string) bool {
	supported, ok := Supported[component]
	if !ok {
		return true
	}
	parts := tagVersion.FindStringSubmatch(tag)
	if parts == nil {
		return false
	}
	var numbers [3]uint64
	for i := range numbers {
		n, err := strconv.ParseUint(parts[i+1], 10, 64)
		if err != nil {
			return false
		}
		numbers[i] = n
	}
	major, minor, ok := strings.Cut(supported, ".")
	if !ok {
		return false
	}
	maxMajor, majorErr := strconv.ParseUint(major, 10, 64)
	maxMinor, minorErr := strconv.ParseUint(minor, 10, 64)
	return majorErr == nil && minorErr == nil &&
		(numbers[0] < maxMajor || numbers[0] == maxMajor && numbers[1] <= maxMinor)
}
