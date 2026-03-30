package cache

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// durationRe matches day (d) and week (w) components that time.ParseDuration
// does not support.
var durationRe = regexp.MustCompile(`(\d+)([dw])`)

// ParseCacheDuration extends time.ParseDuration with support for day ("d")
// and week ("w") units. Examples: "7d", "2w", "1d12h", "24h".
//
// Day and week components are converted to hours (1d = 24h, 1w = 168h)
// before delegating to time.ParseDuration.
func ParseCacheDuration(input string) (time.Duration, error) {
	if input == "" {
		return 0, errors.New("empty duration string")
	}

	replaced := input

	matches := durationRe.FindAllStringSubmatch(input, -1)
	for _, match := range matches {
		count, err := strconv.Atoi(match[1])
		if err != nil {
			return 0, fmt.Errorf("invalid number in duration %q: %w", input, err)
		}

		var hours int

		switch match[2] {
		case "d":
			hours = count * 24
		case "w":
			hours = count * 7 * 24
		}

		replaced = strings.Replace(replaced, match[0], strconv.Itoa(hours)+"h", 1)
	}

	duration, err := time.ParseDuration(replaced)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q: %w", input, err)
	}

	return duration, nil
}
