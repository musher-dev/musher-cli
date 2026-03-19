package bundledef

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"

	bundledefschema "github.com/musher-dev/musher-cli/schemas/bundledef"
)

var (
	compiledSchema *jsonschema.Schema
	compileOnce    sync.Once
	errCompile     error
)

// ValidationError represents a single schema validation error.
type ValidationError struct {
	Path    string
	Message string
}

func (e ValidationError) String() string {
	if e.Path == "" {
		return e.Message
	}

	return fmt.Sprintf("%s: %s", e.Path, e.Message)
}

func getSchema() (*jsonschema.Schema, error) {
	compileOnce.Do(func() {
		var schemaDoc any
		if err := json.Unmarshal(bundledefschema.V1Alpha1, &schemaDoc); err != nil {
			errCompile = fmt.Errorf("unmarshal schema: %w", err)
			return
		}

		c := jsonschema.NewCompiler()
		if err := c.AddResource("https://schemas.musher.dev/bundledef/v1alpha1.json", schemaDoc); err != nil {
			errCompile = fmt.Errorf("add schema resource: %w", err)
			return
		}

		compiledSchema, errCompile = c.Compile("https://schemas.musher.dev/bundledef/v1alpha1.json")
	})

	if errCompile != nil {
		return nil, fmt.Errorf("compile schema: %w", errCompile)
	}

	return compiledSchema, nil
}

// ValidateSchema validates raw YAML bytes against the bundle definition JSON Schema.
func ValidateSchema(yamlData []byte) []ValidationError {
	schema, err := getSchema()
	if err != nil {
		return []ValidationError{{Message: fmt.Sprintf("schema compilation error: %v", err)}}
	}

	var doc any
	if unmarshalErr := yaml.Unmarshal(yamlData, &doc); unmarshalErr != nil {
		return []ValidationError{{Message: fmt.Sprintf("YAML parse error: %v", unmarshalErr)}}
	}

	// Convert YAML's map[string]any (which yaml.v3 produces) to JSON-compatible types.
	doc = convertYAMLToJSON(doc)

	err = schema.Validate(doc)
	if err == nil {
		return nil
	}

	valErr, ok := err.(*jsonschema.ValidationError) //nolint:errorlint // jsonschema returns *ValidationError directly
	if !ok {
		return []ValidationError{{Message: err.Error()}}
	}

	return flattenErrors(valErr)
}

// flattenErrors extracts leaf validation errors from the jsonschema error tree.
func flattenErrors(valErr *jsonschema.ValidationError) []ValidationError {
	if len(valErr.Causes) == 0 {
		msg := valErr.Error()
		path := "/" + strings.Join(valErr.InstanceLocation, "/")
		if len(valErr.InstanceLocation) == 0 {
			path = ""
		}

		return []ValidationError{{Path: path, Message: msg}}
	}

	var result []ValidationError
	for _, cause := range valErr.Causes {
		result = append(result, flattenErrors(cause)...)
	}

	return result
}

// convertYAMLToJSON recursively converts YAML-decoded values to JSON-compatible types.
// yaml.v3 decodes maps as map[string]any and integers as int, but jsonschema
// expects float64 for numbers (as encoding/json does).
func convertYAMLToJSON(v any) any {
	switch val := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(val))
		for k, v := range val {
			out[k] = convertYAMLToJSON(v)
		}

		return out
	case []any:
		out := make([]any, len(val))
		for i, v := range val {
			out[i] = convertYAMLToJSON(v)
		}

		return out
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return val
	}
}
