package validate

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// APIURL validates a configured API base URL.
func APIURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("value cannot be empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse URL: %w", err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("scheme must be http or https")
	}

	if parsed.Host == "" {
		return "", errors.New("host is required")
	}

	return parsed.String(), nil
}
