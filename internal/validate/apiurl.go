// Package validate contains focused validation helpers for user configuration.
package validate

import (
	"errors"
	"net/url"
	"strings"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// APIURL validates a configured API base URL.
func APIURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("value cannot be empty")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "", repoerrors.Errorf("parse URL: %w", err)
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", errors.New("scheme must be http or https")
	}

	if parsed.Host == "" {
		return "", errors.New("host is required")
	}

	return parsed.String(), nil
}
