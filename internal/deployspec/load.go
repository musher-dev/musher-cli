package deployspec

import (
	"bytes"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
	"github.com/musher-dev/musher-cli/internal/safeio"
)

// DefaultFileName is the file `musher deploy` looks for in the working directory.
const DefaultFileName = "musher.yaml"

// alternateFileName is accepted because both spellings are conventional and
// silently ignoring one is a confusing failure.
const alternateFileName = "musher.yml"

// Discover returns the path to the App file in dir, or "" when there is none.
func Discover(dir string) string {
	for _, name := range []string{DefaultFileName, alternateFileName} {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}

	return ""
}

// Load reads, defaults, and validates an App document.
func Load(path string) (*App, error) {
	data, err := safeio.ReadFile(path)
	if err != nil {
		return nil, repoerrors.Errorf("read %s: %w", path, err)
	}

	return Parse(data)
}

// Parse decodes an App document from YAML bytes.
//
// Unknown fields are rejected. A typo like "replica: 3" silently deploying one
// replica is exactly the class of bug a deployment tool must not have.
func Parse(data []byte) (*App, error) {
	var app App

	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)

	if err := decoder.Decode(&app); err != nil {
		return nil, repoerrors.Errorf("parse deployment file: %w", err)
	}

	app.ApplyDefaults()

	if err := app.Validate(); err != nil {
		return nil, err
	}

	return &app, nil
}
