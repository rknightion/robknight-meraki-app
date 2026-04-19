package recordings

import (
	"encoding/json"
	"strings"
	"testing"
	"testing/fstest"
)

// TestLoadRegistry confirms the production embedded FS parses cleanly and
// that the first seed template is always present. The stricter per-template
// render contract lands with the golden-fixture suite in §4.6.2; this test
// guards structural regressions only (the FS fails to load, groups go
// missing, the metric-name contract fails).
func TestLoadRegistry(t *testing.T) {
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	groups := reg.Groups()
	if len(groups) == 0 {
		t.Fatalf("expected at least 1 group, got 0")
	}
	tpl, ok := reg.Template("availability", "device-status-overview")
	if !ok {
		t.Fatalf("Template(availability, device-status-overview): not found (groups=%v)", groups)
	}
	if tpl.Metric != "meraki_device_status_count" {
		t.Fatalf("device-status-overview metric = %q, want meraki_device_status_count", tpl.Metric)
	}
	for _, g := range groups {
		for _, t2 := range g.Templates {
			if t2.GroupID == "" || t2.ID == "" {
				t.Errorf("group %q: template with empty ID/GroupID: %+v", g.ID, t2)
			}
			if !metricNameRE.MatchString(t2.Metric) {
				t.Errorf("%s/%s: metric %q does not match %s", g.ID, t2.ID, t2.Metric, metricNameRE)
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
	if len(g.Templates) == 0 || g.Templates[0].ID != "device-status-overview" {
		t.Fatalf("unexpected templates under availability: %+v", g.Templates)
	}
	tpl, ok := reg.Template("availability", "device-status-overview")
	if !ok {
		t.Fatal("Template(availability, device-status-overview): not found")
	}
	if tpl.Metric == "" {
		t.Fatal("expected non-empty metric name")
	}
	if _, ok := reg.Template("missing", "x"); ok {
		t.Fatal("Template(missing, x): unexpected hit")
	}
	if _, ok := reg.Group("missing"); ok {
		t.Fatal("Group(missing): unexpected hit")
	}
}

// TestRegistry_duplicate_detection feeds LoadRegistryFS two YAML files
// that collide on (group, id). The loader must refuse.
func TestRegistry_duplicate_detection(t *testing.T) {
	dup := []byte(`kind: recording_rule_template
id: dup
group: availability
display_name: Dup
thresholds: []
rule:
  title: "x"
  data: []
  record:
    metric: meraki_availability_dup
    from: A
  labels:
    managed_by: meraki-plugin
    meraki_kind: recording
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
// without `kind: recording_rule_template` is a bug in the template author's
// YAML, not something we silently tolerate.
func TestRegistry_rejects_unknown_kind(t *testing.T) {
	fs := fstest.MapFS{
		"availability/bad.yaml": &fstest.MapFile{Data: []byte(`kind: alert_rule_template
id: x
group: availability
rule:
  record:
    metric: meraki_availability_x
    from: A
`)},
	}
	_, err := LoadRegistryFS(fs)
	if err == nil || !strings.Contains(err.Error(), "recording_rule_template") {
		t.Fatalf("expected kind-mismatch error, got %v", err)
	}
}

// TestRegistry_rejects_missing_record enforces the recording-specific
// constraint that every template MUST declare a `rule.record` block with
// at least `metric` + `from`. Without this, the renderer has no idea what
// to emit, and the omission would only surface at Render time — we catch
// it at load time instead.
func TestRegistry_rejects_missing_record(t *testing.T) {
	fs := fstest.MapFS{
		"availability/no-record.yaml": &fstest.MapFile{Data: []byte(`kind: recording_rule_template
id: no-record
group: availability
rule:
  title: "x"
  data: []
`)},
	}
	_, err := LoadRegistryFS(fs)
	if err == nil || !strings.Contains(err.Error(), "record") {
		t.Fatalf("expected missing-record error, got %v", err)
	}
}

// TestRegistry_rejects_bad_metric_name enforces the
// `meraki_<something>` snake_case metric-name contract. Templates that
// slip in a non-conforming metric (uppercase, dashes, missing prefix)
// should be a startup error, not a silent write of a badly-named series.
func TestRegistry_rejects_bad_metric_name(t *testing.T) {
	fs := fstest.MapFS{
		"availability/bad-metric.yaml": &fstest.MapFile{Data: []byte(`kind: recording_rule_template
id: bad-metric
group: availability
rule:
  title: "x"
  data: []
  record:
    metric: Bad-Metric-Name
    from: A
`)},
	}
	_, err := LoadRegistryFS(fs)
	if err == nil || !strings.Contains(err.Error(), "metric") {
		t.Fatalf("expected metric-name error, got %v", err)
	}
}

// TestTemplate_Render_minimal exercises the happy path end-to-end using the
// real embedded registry. Verifies that Render produces an AlertRule with:
//   - UID meraki-rec-availability-device-status-overview-<org>
//   - For = "0s"
//   - Record block populated with metric + from + TargetDatasourceUID
//   - No Condition / NoDataState / ExecErrState (those are alert-only)
//   - FolderUID + RuleGroup backfilled.
//
// Full golden-fixture coverage (structural + field-by-field) arrives with
// §4.6.2. This is the smoke that guarantees the package compiles and the
// renderer emits a shape Grafana will accept.
func TestTemplate_Render_minimal(t *testing.T) {
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	tpl, ok := reg.Template("availability", "device-status-overview")
	if !ok {
		t.Fatal("template not found")
	}

	rule, err := tpl.Render("987654", nil, "my-prom-uid")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if want := "meraki-rec-availability-device-status-overview-987654"; rule.UID != want {
		t.Errorf("UID = %q, want %q", rule.UID, want)
	}
	if rule.For != "0s" {
		t.Errorf("For = %q, want 0s", rule.For)
	}
	if rule.FolderUID != bundledRecordingsFolderUID {
		t.Errorf("FolderUID = %q, want %q", rule.FolderUID, bundledRecordingsFolderUID)
	}
	if rule.RuleGroup != "availability" {
		t.Errorf("RuleGroup = %q, want availability", rule.RuleGroup)
	}
	if rule.Record == nil {
		t.Fatal("Record is nil")
	}
	if rule.Record.Metric != "meraki_device_status_count" {
		t.Errorf("Record.Metric = %q, want meraki_device_status_count", rule.Record.Metric)
	}
	if rule.Record.From != "A" {
		t.Errorf("Record.From = %q, want A", rule.Record.From)
	}
	if rule.Record.TargetDatasourceUID != "my-prom-uid" {
		t.Errorf("Record.TargetDatasourceUID = %q, want my-prom-uid", rule.Record.TargetDatasourceUID)
	}
	if rule.Condition != "" || rule.NoDataState != "" || rule.ExecErrState != "" {
		t.Errorf("alert-only fields must remain empty for recording rules: condition=%q nodata=%q execerr=%q",
			rule.Condition, rule.NoDataState, rule.ExecErrState)
	}
	if lbl := rule.Labels["meraki_kind"]; lbl != "recording" {
		t.Errorf("Labels[meraki_kind] = %q, want recording", lbl)
	}
	if lbl := rule.Labels["managed_by"]; lbl != "meraki-plugin" {
		t.Errorf("Labels[managed_by] = %q, want meraki-plugin", lbl)
	}
	if lbl := rule.Labels["meraki_org"]; lbl != "987654" {
		t.Errorf("Labels[meraki_org] = %q, want 987654", lbl)
	}

	// Spot-check the JSON round-trip: the omitempty tags on alert-only
	// fields must actually drop them from the wire payload, otherwise
	// Grafana will reject the recording rule with a 400.
	raw, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	for _, banned := range []string{`"condition"`, `"noDataState"`, `"execErrState"`} {
		if strings.Contains(s, banned) {
			t.Errorf("serialised recording rule must not contain %s: %s", banned, s)
		}
	}
	if !strings.Contains(s, `"record"`) {
		t.Errorf("serialised recording rule must contain record block: %s", s)
	}
	if !strings.Contains(s, `"target_datasource_uid":"my-prom-uid"`) {
		t.Errorf("serialised recording rule must embed target_datasource_uid: %s", s)
	}
}

// TestTemplate_Render_requires_target_ds confirms that Render refuses an
// empty target datasource UID — the contract with the reconciler is that
// it MUST pass through the operator's selection, not silently fall back.
func TestTemplate_Render_requires_target_ds(t *testing.T) {
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	tpl, _ := reg.Template("availability", "device-status-overview")
	if _, err := tpl.Render("987654", nil, ""); err == nil {
		t.Fatal("expected Render to error on empty targetDsUID")
	}
}

// TestTemplate_Render_requires_org_id symmetrically confirms the orgID
// guard — Render is used from a fan-out loop so an empty org is always
// a caller bug.
func TestTemplate_Render_requires_org_id(t *testing.T) {
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	tpl, _ := reg.Template("availability", "device-status-overview")
	if _, err := tpl.Render("", nil, "my-uid"); err == nil {
		t.Fatal("expected Render to error on empty orgID")
	}
}
