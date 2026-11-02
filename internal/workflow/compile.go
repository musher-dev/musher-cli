package workflow

import (
	"regexp"
	"sort"
	"strings"

	"github.com/musher-dev/musher-cli/internal/client"
	"github.com/musher-dev/musher-cli/internal/deployspec"
	repoerrors "github.com/musher-dev/musher-cli/internal/errors"
)

// envInputPrefix namespaces the contract inputs compiled from env vars, so a
// key like PORT becomes the input envPort and can never collide with an input
// a later musher.yaml feature declares.
const envInputPrefix = "env"

// maxInputNameLength mirrors the contract's input-name maxLength.
const maxInputNameLength = 64

// inputNameRE mirrors the contract's input-name pattern.
var inputNameRE = regexp.MustCompile(`^[a-z][a-zA-Z0-9]{0,63}$`)

// DeployEndpoint is one port the workload serves.
type DeployEndpoint struct {
	// TargetPort is the container port.
	TargetPort int
	// Protocol is HTTP, HTTPS, WS, GRPC, TCP or UDP. Empty means HTTP.
	Protocol string
	// Visibility is PUBLIC or PRIVATE. Empty means PUBLIC for a SERVICE and
	// PRIVATE otherwise.
	Visibility string
}

// provenance maps JSON pointers in a compiled document back to the
// musher.yaml paths they came from, so a server diagnostic can name the line
// a user actually wrote.
type provenance map[string]string

// lookup returns the musher.yaml path for pointer, walking up to the nearest
// mapped ancestor. It reports false when nothing in the user's file explains
// the pointer — a field the CLI generated on its own.
func (p provenance) lookup(pointer string) (string, bool) {
	for current := pointer; current != ""; current = parentPointer(current) {
		if path, ok := p[current]; ok {
			return path, true
		}
	}

	return "", false
}

// parentPointer drops the last reference token of a JSON pointer.
func parentPointer(pointer string) string {
	index := strings.LastIndexByte(pointer, '/')
	if index <= 0 {
		return ""
	}

	return pointer[:index]
}

// pointerToken escapes one reference token per RFC 6901.
func pointerToken(token string) string {
	return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1")
}

// compiled is a deploy's intent expanded into platform documents.
type compiled struct {
	// slug names the component, the blueprint, the node, and the deployment.
	slug   string
	kind   string
	inputs map[string]string // input name -> env key
	input  *DeployInput

	componentSource provenance
	blueprintSource provenance
}

// compile validates the mapping from intent onto documents. It needs no
// network, so a bad mapping fails before any request is made.
func compile(input *DeployInput) (*compiled, error) {
	kind := strings.ToUpper(strings.TrimSpace(input.Kind))
	if kind == "" {
		kind = client.ComponentTypeService
	}

	plan := &compiled{
		slug:            input.Name,
		kind:            kind,
		inputs:          make(map[string]string, len(input.Env)),
		input:           input,
		componentSource: provenance{},
		blueprintSource: provenance{},
	}

	for _, key := range sortedKeys(input.Env) {
		name := envInputName(key)
		if len(name) > maxInputNameLength || !inputNameRE.MatchString(name) {
			return nil, repoerrors.Errorf(
				"env var %q cannot be expressed as a platform input (derived name %q); shorten or rename it", key, name)
		}

		if previous, clash := plan.inputs[name]; clash {
			return nil, repoerrors.Errorf(
				"env vars %q and %q map to the same platform input %q; rename one of them", previous, key, name)
		}

		plan.inputs[name] = key
	}

	if kind == client.ComponentTypeJob && len(input.Endpoints) > 0 {
		return nil, repoerrors.Errorf("a JOB serves no endpoints; remove them or change the kind")
	}

	plan.buildProvenance()

	return plan, nil
}

// envInputName derives the contract input name for an env key:
// DATABASE_URL becomes envDatabaseUrl.
func envInputName(key string) string {
	var builder strings.Builder

	builder.WriteString(envInputPrefix)

	for part := range strings.SplitSeq(strings.ToLower(key), "_") {
		if part == "" {
			continue
		}

		builder.WriteString(strings.ToUpper(part[:1]))
		builder.WriteString(part[1:])
	}

	return builder.String()
}

// buildProvenance records where every compiled field came from.
func (c *compiled) buildProvenance() {
	comp := c.componentSource
	comp["/spec/type"] = "spec.workload.kind"
	comp["/spec/workload"] = "spec.workload"
	comp["/spec/workload/source"] = "spec.workload.image"
	comp["/spec/workload/command"] = "spec.workload.command"
	comp["/spec/workload/schedule"] = "spec.workload.schedule"
	comp["/spec/workload/schedule/cron"] = "spec.workload.schedule.cron"
	comp["/spec/workload/endpoints"] = "spec.workload.endpoints"
	comp["/spec/contract/inputs"] = "spec.workload.env"

	for _, name := range sortedKeys(c.input.Endpoints) {
		pointer := "/spec/workload/endpoints/" + pointerToken(name)
		path := "spec.workload.endpoints." + name
		comp[pointer] = path
		comp[pointer+"/targetPort"] = path + ".targetPort"
		comp[pointer+"/protocol"] = path + ".protocol"
	}

	node := "/spec/nodes/" + pointerToken(c.slug)
	blue := c.blueprintSource
	blue["/metadata/slug"] = "metadata.name"
	blue[node] = "metadata.name"
	blue[node+"/compute"] = "spec.size"
	blue[node+"/exposure"] = "spec.workload.endpoints"

	for _, name := range sortedKeys(c.input.Endpoints) {
		blue[node+"/exposure/"+pointerToken(name)] = "spec.workload.endpoints." + name + ".visibility"
	}

	for name, key := range c.inputs {
		comp["/spec/contract/inputs/"+pointerToken(name)] = "spec.workload.env." + key
		blue[node+"/bindings/"+pointerToken(name)] = "spec.workload.env." + key
	}
}

// component renders the component document at revision.
func (c *compiled) component(revision int) *client.ComponentDocument {
	workload := &client.ComponentWorkload{
		Source:  &client.ComponentSource{Image: strings.TrimSpace(c.input.Image)},
		Command: c.input.Command,
	}

	if c.kind == client.ComponentTypeJob {
		if cron := strings.TrimSpace(c.input.Schedule); cron != "" {
			workload.Schedule = &client.ComponentSchedule{Cron: cron}
		}
	} else if len(c.input.Endpoints) > 0 {
		workload.Endpoints = make(map[string]client.ComponentEndpoint, len(c.input.Endpoints))

		for name, endpoint := range c.input.Endpoints {
			workload.Endpoints[name] = client.ComponentEndpoint{
				TargetPort: endpoint.TargetPort,
				Protocol:   protocolOrDefault(endpoint.Protocol),
			}
		}
	}

	doc := &client.ComponentDocument{
		SpecVersion: client.SpecVersionV2,
		Kind:        client.DocumentKindComponent,
		Metadata: client.ComponentDocumentMetadata{
			Revision:    revision,
			Description: "The " + c.slug + " workload, deployed with musher deploy.",
		},
		Spec: client.ComponentDocumentSpec{
			Type:     c.kind,
			Workload: workload,
		},
	}

	if len(c.inputs) > 0 {
		inputs := make(map[string]client.ComponentInput, len(c.inputs))

		for name, key := range c.inputs {
			inputs[name] = client.ComponentInput{
				Description: "Value of the " + key + " environment variable.",
				Schema:      client.ComponentValueSchema{Type: client.ValueTypeString},
				Target:      &client.ComponentInputTarget{EnvVar: key},
			}
		}

		doc.Spec.Contract = &client.ComponentContract{Inputs: inputs}
	}

	return doc
}

// blueprint renders the single-node blueprint document at revision, pinning
// the component revision it runs.
func (c *compiled) blueprint(revision int, componentID string, componentRevision int, profile string) *client.BlueprintDocument {
	node := client.BlueprintNode{
		ComponentRef: componentID,
		Revision:     componentRevision,
	}

	if profile != "" {
		node.Compute = &client.BlueprintCompute{Profile: profile}
	}

	if c.kind != client.ComponentTypeJob && len(c.input.Endpoints) > 0 {
		node.Exposure = make(map[string]string, len(c.input.Endpoints))

		for name, endpoint := range c.input.Endpoints {
			visibility := strings.ToUpper(strings.TrimSpace(endpoint.Visibility))
			if visibility == "" {
				visibility = deployspec.DefaultVisibility(c.kind)
			}

			node.Exposure[name] = visibility
		}
	}

	if len(c.inputs) > 0 {
		node.Bindings = make(map[string]client.BlueprintValueBinding, len(c.inputs))

		for name, key := range c.inputs {
			node.Bindings[name] = client.BlueprintValueBinding{Value: c.input.Env[key]}
		}
	}

	return &client.BlueprintDocument{
		SpecVersion: client.SpecVersionV2,
		Kind:        client.DocumentKindBlueprint,
		Metadata: client.BlueprintDocumentMetadata{
			Slug:        c.slug,
			Revision:    revision,
			Description: c.slug + ", deployed with musher deploy.",
		},
		Spec: client.BlueprintDocumentSpec{
			Nodes: map[string]client.BlueprintNode{c.slug: node},
		},
	}
}

func protocolOrDefault(protocol string) string {
	trimmed := strings.ToUpper(strings.TrimSpace(protocol))
	if trimmed == "" {
		return "HTTP"
	}

	return trimmed
}

// sortedKeys returns a map's keys in order, so compiled output and the
// diagnostics derived from it are deterministic.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}
