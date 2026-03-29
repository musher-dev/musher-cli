package harness

// Module bundles a provider spec with its factory function.
// Each harness provider package exports a Module variable that the
// central builtins registration uses to populate the Registry.
//
// To add a new harness:
//  1. Create internal/harness/provider/<name>/spec.yaml
//  2. Create internal/harness/provider/<name>/provider.go (embed spec, export Module)
//  3. Create internal/harness/provider/<name>/executor.go (when executor interface exists)
//  4. Add <name>.Module to the builtins list in RegisterBuiltins
type Module struct {
	Spec *Spec
	// NewExecutor will be added when the executor interface is defined.
	// For now, Module carries only the spec.
}

// RegisterBuiltins populates a Registry from a list of modules.
func RegisterBuiltins(reg *Registry, modules ...*Module) {
	for _, mod := range modules {
		reg.Register(&Provider{
			Spec:      mod.Spec,
			Available: DefaultAvailableFunc(mod.Spec.Binary),
		})
	}
}
