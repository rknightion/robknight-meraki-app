package recordings

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/robknight/grafana-meraki-plugin/pkg/plugin/alerts"
)

// stubMeraki is a nil-safe Meraki stub. Returns whatever orgs the test set.
// Mirrors the shape of alerts/reconciler_test.go's stubMeraki so both
// packages' test harnesses are recognisable at a glance.
type stubMeraki struct {
	orgs []alerts.Organization
	err  error
}

func (s stubMeraki) GetOrganizations(_ context.Context) ([]alerts.Organization, error) {
	return s.orgs, s.err
}

// testReg loads the embedded registry once per test. Phase 1 only ships
// availability/device-status-overview, so the tests build DesiredState
// referencing that template.
func testReg(t *testing.T) *Registry {
	t.Helper()
	r, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	return r
}

// testSetup wires an alerts.InMemoryGrafana stub + neutered batch stagger so
// the batch-apply loop exercises without blocking the suite on timer.After.
// Returned stub is reused across the whole test so pre-seeding for scenarios
// is possible without constructing a second instance.
func testSetup(t *testing.T) *alerts.InMemoryGrafana {
	t.Helper()
	// Eliminate the 100ms stagger for tests by default — the batch-stagger
	// scenario flips this back to a measurable value within its own body.
	prev := reconcileBatchStagger
	reconcileBatchStagger = 0
	t.Cleanup(func() { reconcileBatchStagger = prev })
	return alerts.NewInMemoryGrafana()
}

// desiredDeviceStatus is the default DesiredState for most tests: the single
// Phase 1 template installed in the single group, with the operator's
// target-datasource UID populated (no test should depend on default value).
func desiredDeviceStatus(targetDsUID string) DesiredState {
	return DesiredState{
		Groups: map[string]GroupState{
			"availability": {
				Installed:    true,
				RulesEnabled: map[string]bool{"device-status-overview": true},
			},
		},
		TargetDsUID: targetDsUID,
	}
}

// countingGrafana wraps alerts.InMemoryGrafana to count every CRUD call.
// Needed for tests that assert "second run performs zero writes" — the raw
// InMemoryGrafana is a storage stub and doesn't expose call counters.
type countingGrafana struct {
	*alerts.InMemoryGrafana
	creates int
	updates int
	deletes int
	folders int
	lists   int
}

func newCounting(inner *alerts.InMemoryGrafana) *countingGrafana {
	return &countingGrafana{InMemoryGrafana: inner}
}

func (c *countingGrafana) EnsureFolder(ctx context.Context, uid, title string) error {
	c.folders++
	return c.InMemoryGrafana.EnsureFolder(ctx, uid, title)
}

func (c *countingGrafana) ListAlertRules(ctx context.Context, folderUID string) ([]alerts.AlertRule, error) {
	c.lists++
	return c.InMemoryGrafana.ListAlertRules(ctx, folderUID)
}

func (c *countingGrafana) CreateAlertRule(ctx context.Context, r alerts.AlertRule) error {
	c.creates++
	return c.InMemoryGrafana.CreateAlertRule(ctx, r)
}

func (c *countingGrafana) UpdateAlertRule(ctx context.Context, uid string, r alerts.AlertRule) error {
	c.updates++
	return c.InMemoryGrafana.UpdateAlertRule(ctx, uid, r)
}

func (c *countingGrafana) DeleteAlertRule(ctx context.Context, uid string) error {
	c.deletes++
	return c.InMemoryGrafana.DeleteAlertRule(ctx, uid)
}

// resetCounts zeroes the CRUD counters so a test can measure the delta of a
// follow-up Reconcile without fresh-storage plumbing.
func (c *countingGrafana) resetCounts() {
	c.creates = 0
	c.updates = 0
	c.deletes = 0
	c.folders = 0
	c.lists = 0
}

func TestReconcile_happy_path(t *testing.T) {
	stub := testSetup(t)
	g := newCounting(stub)
	reg := testReg(t)

	meraki := stubMeraki{orgs: []alerts.Organization{
		{ID: "111"}, {ID: "222"},
	}}
	res, err := Reconcile(context.Background(), g, meraki, reg, desiredDeviceStatus("prom-uid"))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n := len(res.Created); n != 2 {
		t.Fatalf("Created = %d, want 2 (res=%+v)", n, res)
	}
	if g.creates != 2 {
		t.Fatalf("CreateAlertRule calls = %d, want 2", g.creates)
	}
	if g.updates != 0 || g.deletes != 0 {
		t.Fatalf("unexpected UPDATE/DELETE: updates=%d deletes=%d", g.updates, g.deletes)
	}
	if n := len(res.Failed); n != 0 {
		t.Fatalf("unexpected Failed = %+v", res.Failed)
	}
}

func TestReconcile_is_idempotent(t *testing.T) {
	stub := testSetup(t)
	g := newCounting(stub)
	reg := testReg(t)

	meraki := stubMeraki{orgs: []alerts.Organization{{ID: "111"}, {ID: "222"}}}
	if _, err := Reconcile(context.Background(), g, meraki, reg, desiredDeviceStatus("prom-uid")); err != nil {
		t.Fatalf("Reconcile#1: %v", err)
	}
	g.resetCounts()

	res, err := Reconcile(context.Background(), g, meraki, reg, desiredDeviceStatus("prom-uid"))
	if err != nil {
		t.Fatalf("Reconcile#2: %v", err)
	}
	if n := len(res.Created) + len(res.Updated) + len(res.Deleted); n != 0 {
		t.Fatalf("Reconcile#2 should be no-op, got res=%+v", res)
	}
	if g.creates != 0 || g.updates != 0 || g.deletes != 0 {
		t.Fatalf("Reconcile#2 produced writes: creates=%d updates=%d deletes=%d",
			g.creates, g.updates, g.deletes)
	}
}

func TestReconcile_target_ds_uid_change_triggers_update(t *testing.T) {
	stub := testSetup(t)
	g := newCounting(stub)
	reg := testReg(t)

	meraki := stubMeraki{orgs: []alerts.Organization{{ID: "111"}, {ID: "222"}}}
	if _, err := Reconcile(context.Background(), g, meraki, reg, desiredDeviceStatus("prom-uid-A")); err != nil {
		t.Fatalf("Reconcile#1: %v", err)
	}
	g.resetCounts()

	// Flip the target DS UID — every rule's Record block changes, so
	// ruleSignature must produce a different signature and force an UPDATE
	// for every existing rule.
	res, err := Reconcile(context.Background(), g, meraki, reg, desiredDeviceStatus("prom-uid-B"))
	if err != nil {
		t.Fatalf("Reconcile#2: %v", err)
	}
	if n := len(res.Updated); n != 2 {
		t.Fatalf("Updated = %d, want 2 (res=%+v)", n, res)
	}
	if n := len(res.Created); n != 0 {
		t.Fatalf("Created = %d, want 0 (TargetDsUID change should not re-create)", n)
	}
	if g.updates != 2 {
		t.Fatalf("UpdateAlertRule calls = %d, want 2", g.updates)
	}
}

func TestReconcile_disabled_group_deletes_managed_rules(t *testing.T) {
	stub := testSetup(t)
	g := newCounting(stub)
	reg := testReg(t)

	meraki := stubMeraki{orgs: []alerts.Organization{{ID: "111"}, {ID: "222"}}}
	if _, err := Reconcile(context.Background(), g, meraki, reg, desiredDeviceStatus("prom-uid")); err != nil {
		t.Fatalf("Reconcile#1: %v", err)
	}
	g.resetCounts()

	// Toggle the group off. Grafana still has the rules; reconciler must
	// delete every rule matching the label gate and leave unrelated rules
	// alone. Setting RulesEnabled keeps the intent explicit — just
	// Installed=false is the "uninstall the whole group" path regardless
	// of per-template toggle state.
	desired := desiredDeviceStatus("prom-uid")
	desired.Groups["availability"] = GroupState{
		Installed:    false,
		RulesEnabled: map[string]bool{"device-status-overview": true},
	}

	res, err := Reconcile(context.Background(), g, meraki, reg, desired)
	if err != nil {
		t.Fatalf("Reconcile#2: %v", err)
	}
	if n := len(res.Deleted); n != 2 {
		t.Fatalf("Deleted = %d, want 2 (res=%+v)", n, res)
	}
	if n := len(res.Created) + len(res.Updated); n != 0 {
		t.Fatalf("Created+Updated = %d, want 0", n)
	}
}

func TestReconcile_preserves_is_paused(t *testing.T) {
	stub := testSetup(t)
	g := newCounting(stub)
	reg := testReg(t)

	meraki := stubMeraki{orgs: []alerts.Organization{{ID: "111"}}}
	if _, err := Reconcile(context.Background(), g, meraki, reg, desiredDeviceStatus("prom-uid")); err != nil {
		t.Fatalf("Reconcile#1: %v", err)
	}

	// Operator pauses the rule via Grafana's UI. Mutate the underlying
	// store directly to simulate that.
	rules, err := stub.ListAlertRules(context.Background(), bundledRecordingsFolderUID)
	if err != nil {
		t.Fatalf("ListAlertRules: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule after reconcile, got %d", len(rules))
	}
	paused := rules[0]
	paused.IsPaused = true
	if err := stub.UpdateAlertRule(context.Background(), paused.UID, paused); err != nil {
		t.Fatalf("UpdateAlertRule to pause: %v", err)
	}
	g.resetCounts()

	// A no-op reconcile MUST NOT issue an UPDATE that would blow away the
	// operator's pause. IsPaused is intentionally outside ruleSignature.
	res, err := Reconcile(context.Background(), g, meraki, reg, desiredDeviceStatus("prom-uid"))
	if err != nil {
		t.Fatalf("Reconcile#2: %v", err)
	}
	if n := len(res.Created) + len(res.Updated) + len(res.Deleted); n != 0 {
		t.Fatalf("Reconcile#2 should be no-op after pause, got res=%+v", res)
	}
	if g.updates != 0 {
		t.Fatalf("unexpected UPDATE on no-op: %d", g.updates)
	}
	// Confirm the stored rule retains IsPaused=true.
	rules2, err := stub.ListAlertRules(context.Background(), bundledRecordingsFolderUID)
	if err != nil {
		t.Fatalf("ListAlertRules: %v", err)
	}
	if len(rules2) != 1 || !rules2[0].IsPaused {
		t.Fatalf("paused state lost: rules=%+v", rules2)
	}
}

func TestReconcile_label_gate_ignores_user_rules(t *testing.T) {
	// Two scenarios — both must leave the user's rule alone when desired
	// state is empty:
	//   (a) rule carries meraki_kind=recording but lacks managed_by=meraki-plugin
	//   (b) rule carries managed_by=meraki-plugin but lacks meraki_kind=recording
	cases := []struct {
		name   string
		labels map[string]string
	}{
		{
			name:   "missing_managed_by",
			labels: map[string]string{"meraki_kind": "recording", "meraki_org": "000"},
		},
		{
			name:   "missing_meraki_kind",
			labels: map[string]string{"managed_by": "meraki-plugin", "meraki_org": "000"},
		},
		{
			name:   "both_missing",
			labels: map[string]string{"meraki_org": "000"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := testSetup(t)
			g := newCounting(stub)
			reg := testReg(t)

			// Seed a rule that LOOKS like one of ours (meraki-rec- UID prefix
			// and the target folder) but whose labels do not satisfy both gates.
			poser := alerts.AlertRule{
				UID:       "meraki-rec-availability-device-status-overview-000",
				Title:     "User's own rule",
				FolderUID: bundledRecordingsFolderUID,
				Labels:    tc.labels,
			}
			if err := stub.CreateAlertRule(context.Background(), poser); err != nil {
				t.Fatalf("seed poser: %v", err)
			}

			// Empty desired state + the org that matches the poser's UID.
			empty := DesiredState{
				Groups:      map[string]GroupState{},
				OrgOverride: []string{"000"},
				TargetDsUID: "prom-uid",
			}

			res, err := Reconcile(context.Background(), g, nil, reg, empty)
			if err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if n := len(res.Deleted); n != 0 {
				t.Fatalf("Deleted = %d, want 0 (label gate failed — would have wiped user rule)", n)
			}
			rules, _ := stub.ListAlertRules(context.Background(), bundledRecordingsFolderUID)
			if len(rules) != 1 {
				t.Fatalf("poser rule was deleted despite label-gate miss; rules=%+v", rules)
			}
		})
	}
}

func TestReconcile_empty_target_ds_uid_is_a_top_level_error(t *testing.T) {
	stub := testSetup(t)
	g := newCounting(stub)
	reg := testReg(t)

	desired := DesiredState{
		Groups: map[string]GroupState{
			"availability": {
				Installed:    true,
				RulesEnabled: map[string]bool{"device-status-overview": true},
			},
		},
		// TargetDsUID deliberately empty.
	}

	res, err := Reconcile(context.Background(), g,
		stubMeraki{orgs: []alerts.Organization{{ID: "111"}}}, reg, desired)
	if err == nil {
		t.Fatalf("expected top-level error on empty TargetDsUID, got nil (res=%+v)", res)
	}
	if !strings.Contains(err.Error(), "TargetDsUID") {
		t.Fatalf("error message should mention TargetDsUID, got %q", err)
	}
	if g.creates != 0 || g.updates != 0 || g.deletes != 0 || g.folders != 0 || g.lists != 0 {
		t.Fatalf("stub saw CRUD calls despite precondition failure: creates=%d updates=%d deletes=%d folders=%d lists=%d",
			g.creates, g.updates, g.deletes, g.folders, g.lists)
	}
}

func TestReconcile_org_override_beats_meraki_api(t *testing.T) {
	stub := testSetup(t)
	g := newCounting(stub)
	reg := testReg(t)

	// Meraki would return three orgs; the override only lists two. Override
	// wins, so only two rules are created — proves the override short-
	// circuits the MerakiAPI call entirely.
	meraki := stubMeraki{orgs: []alerts.Organization{
		{ID: "from-meraki-1"}, {ID: "from-meraki-2"}, {ID: "from-meraki-3"},
	}}
	desired := desiredDeviceStatus("prom-uid")
	desired.OrgOverride = []string{"override-A", "override-B"}

	res, err := Reconcile(context.Background(), g, meraki, reg, desired)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n := len(res.Created); n != 2 {
		t.Fatalf("Created = %d, want 2 (res=%+v)", n, res)
	}
	// Sanity: neither "from-meraki-*" made it into a rule UID.
	for _, uid := range res.Created {
		if strings.Contains(uid, "from-meraki") {
			t.Fatalf("override did not short-circuit Meraki call; got UID %q", uid)
		}
	}
}

// malformedMerakiAPI forces Render to fail by returning an Organization whose
// ID is empty. The registry's Render requires a non-empty orgID, so the
// (org, template) tuple falls into Failed rather than aborting the loop.
type malformedMerakiAPI struct{}

func (malformedMerakiAPI) GetOrganizations(_ context.Context) ([]alerts.Organization, error) {
	return []alerts.Organization{{ID: ""}, {ID: "111"}}, nil
}

func TestReconcile_render_error_is_recorded_not_fatal(t *testing.T) {
	stub := testSetup(t)
	g := newCounting(stub)
	reg := testReg(t)

	res, err := Reconcile(context.Background(), g, malformedMerakiAPI{}, reg, desiredDeviceStatus("prom-uid"))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n := len(res.Failed); n != 1 {
		t.Fatalf("Failed = %d, want 1 (res=%+v)", n, res)
	}
	if res.Failed[0].Action != "render" {
		t.Fatalf("Failed[0].Action = %q, want render", res.Failed[0].Action)
	}
	// The other org should still have rendered + been created.
	if n := len(res.Created); n != 1 {
		t.Fatalf("Created = %d, want 1 (the well-formed org should still land)", n)
	}
	if !strings.Contains(res.Created[0], "-111") {
		t.Fatalf("well-formed org should be in Created; got %v", res.Created)
	}
}

func TestReconcile_batch_stagger(t *testing.T) {
	// Restore the package-level stagger to a measurable value for this
	// test only. Setting it tiny (1ms) lets us exercise the batch-cross
	// code path without making the test slow: we just need the select
	// to fire and not hang.
	prev := reconcileBatchStagger
	reconcileBatchStagger = 1
	t.Cleanup(func() { reconcileBatchStagger = prev })

	// Shrink the batch size too so the loop crosses a batch boundary even
	// with only two orgs. Saves us from needing to fake 25+ orgs in the
	// Meraki stub.
	prevSize := reconcileBatchSize
	reconcileBatchSize = 1
	t.Cleanup(func() { reconcileBatchSize = prevSize })

	stub := alerts.NewInMemoryGrafana()
	g := newCounting(stub)
	reg := testReg(t)

	meraki := stubMeraki{orgs: []alerts.Organization{{ID: "111"}, {ID: "222"}}}
	res, err := Reconcile(context.Background(), g, meraki, reg, desiredDeviceStatus("prom-uid"))
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n := len(res.Created); n != 2 {
		t.Fatalf("Created = %d, want 2 (stagger should not eat rules)", n)
	}
	if g.creates != 2 {
		t.Fatalf("CreateAlertRule calls = %d, want 2", g.creates)
	}
}

// TestReconcile_payload_has_no_alert_only_fields is a sanity check that the
// JSON payload the reconciler would PUT/POST never contains Condition,
// NoDataState, or ExecErrState — Grafana rejects those keys on recording
// rule submissions. The AlertRule struct uses `omitempty` tags to drop
// them; this test guards against an accidental regression in either the
// struct tags or a renderer that starts filling those fields in.
func TestReconcile_payload_has_no_alert_only_fields(t *testing.T) {
	reg := testReg(t)
	tpl, ok := reg.Template("availability", "device-status-overview")
	if !ok {
		t.Fatal("template not found")
	}
	rule, err := tpl.Render("111", nil, "prom-uid")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	raw, err := json.Marshal(rule)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	for _, banned := range []string{`"condition"`, `"noDataState"`, `"execErrState"`} {
		if strings.Contains(s, banned) {
			t.Errorf("recording payload must not contain %s: %s", banned, s)
		}
	}
	// And the inverse — Record block must be present with the wire name
	// Grafana expects.
	if !strings.Contains(s, `"record"`) {
		t.Errorf("payload missing record block: %s", s)
	}
	if !strings.Contains(s, `"target_datasource_uid":"prom-uid"`) {
		t.Errorf("payload missing target_datasource_uid: %s", s)
	}
}

// sanityCheck_orgResolveError asserts resolveOrgs propagates a meraki
// error as a top-level Reconcile failure. Simpler than the alerts-side
// partial-failure harness because our e2e stub can't be forced to 5xx a
// specific request without reinventing the whole thing; an org-resolve
// failure is the easiest top-level error path to exercise here.
func TestReconcile_org_resolve_error_is_top_level(t *testing.T) {
	stub := testSetup(t)
	g := newCounting(stub)
	reg := testReg(t)

	boom := fmt.Errorf("meraki: offline")
	meraki := stubMeraki{err: boom}
	res, err := Reconcile(context.Background(), g, meraki, reg, desiredDeviceStatus("prom-uid"))
	if err == nil {
		t.Fatalf("expected top-level error on org-resolve failure, got nil (res=%+v)", res)
	}
	if !strings.Contains(err.Error(), "meraki: offline") {
		t.Fatalf("expected underlying meraki error to surface, got %q", err)
	}
	if g.creates != 0 || g.folders != 0 || g.lists != 0 {
		t.Fatalf("unexpected writes on top-level failure: %+v", g)
	}
}
