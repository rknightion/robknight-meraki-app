package alerts

import (
	"strings"
	"testing"
	"testing/fstest"
)

// TestLoadRegistry confirms the production embedded FS parses cleanly and
// that the availability/device-offline seed template is always present.
// The stricter per-template render contract is enforced by the
// golden-fixture suite (templates_test.go); this test guards against
// structural regressions (the FS fails to load, groups go missing) only.
func TestLoadRegistry(t *testing.T) {
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	groups := reg.Groups()
	if len(groups) == 0 {
		t.Fatalf("expected at least 1 group, got 0")
	}
	tpl, ok := reg.Template("availability", "device-offline")
	if !ok {
		t.Fatalf("Template(availability, device-offline): not found (groups=%v)", groups)
	}
	if tpl.Severity != "critical" {
		t.Fatalf("device-offline severity = %q, want critical", tpl.Severity)
	}
	// Every loaded template must advertise a non-empty group and ID, and
	// a non-empty severity — guards the YAML-schema contract.
	for _, g := range groups {
		for _, t2 := range g.Templates {
			if t2.GroupID == "" || t2.ID == "" {
				t.Errorf("group %q: template with empty ID/GroupID: %+v", g.ID, t2)
			}
			if t2.Severity == "" {
				t.Errorf("%s/%s: empty severity", g.ID, t2.ID)
			}
		}
	}
}

// TestGroup_Template_lookup exercises the lookup helpers — both positive
// and negative paths.
func TestGroup_Template_lookup(t *testing.T) {
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	g, ok := reg.Group("availability")
	if !ok {
		t.Fatal("Group(availability): not found")
	}
	if len(g.Templates) == 0 || g.Templates[0].ID != "device-offline" {
		t.Fatalf("unexpected templates under availability: %+v", g.Templates)
	}
	tpl, ok := reg.Template("availability", "device-offline")
	if !ok {
		t.Fatal("Template(availability, device-offline): not found")
	}
	if tpl.Severity != "critical" {
		t.Fatalf("expected severity=critical, got %q", tpl.Severity)
	}
	if _, ok := reg.Template("missing", "x"); ok {
		t.Fatal("Template(missing, x): unexpected hit")
	}
	if _, ok := reg.Group("missing"); ok {
		t.Fatal("Group(missing): unexpected hit")
	}
}

// TestRegistry_duplicate_detection feeds LoadRegistryFS two YAML files
// that collide on (group, id). The loader should refuse.
func TestRegistry_duplicate_detection(t *testing.T) {
	dup := []byte(`kind: alert_rule_template
id: device-offline
group: availability
display_name: Device offline
severity: critical
thresholds: []
rule:
  title: "x"
  for: "1m"
  condition: "A"
  data: []
  labels: {}
`)
	fs := fstest.MapFS{
		"availability/a.yaml": &fstest.MapFile{Data: dup},
		"availability/b.yaml": &fstest.MapFile{Data: dup},
	}
	_, err := LoadRegistryFS(fs)
	if err == nil {
		t.Fatal("expected duplicate detection error, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("expected duplicate-detection error, got %v", err)
	}
}

// TestRegistry_rejects_unknown_kind guards the schema contract: a file
// without `kind: alert_rule_template` is a bug in the template author's
// YAML, not something we silently tolerate.
func TestRegistry_rejects_unknown_kind(t *testing.T) {
	fs := fstest.MapFS{
		"availability/bad.yaml": &fstest.MapFile{Data: []byte(`kind: something_else
id: x
group: availability
rule: {}
`)},
	}
	_, err := LoadRegistryFS(fs)
	if err == nil || !strings.Contains(err.Error(), "alert_rule_template") {
		t.Fatalf("expected kind-mismatch error, got %v", err)
	}
}
