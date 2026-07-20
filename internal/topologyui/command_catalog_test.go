package topologyui

import "testing"

func TestCommandCatalogNamesAndAliasesAreUnique(t *testing.T) {
	seen := map[string]string{}
	for _, spec := range commandCatalog {
		if spec.Name == "" || spec.Usage == "" {
			t.Fatalf("incomplete command spec: %#v", spec)
		}
		for _, name := range append([]string{spec.Name}, spec.Aliases...) {
			if previous := seen[name]; previous != "" {
				t.Fatalf("command name %q belongs to %q and %q", name, previous, spec.Name)
			}
			seen[name] = spec.Name
			resolved, ok := resolveCommandSpec(name)
			if !ok || resolved.Name != spec.Name {
				t.Fatalf("resolve %q = %#v, %v", name, resolved, ok)
			}
		}
	}
}
