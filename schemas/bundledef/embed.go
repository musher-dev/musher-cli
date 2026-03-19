// Package bundledefschema embeds the bundle definition JSON Schema.
package bundledefschema

import _ "embed"

// V1Alpha1 contains the embedded v1alpha1 bundle definition JSON Schema.
//
//go:embed v1alpha1.json
var V1Alpha1 []byte
