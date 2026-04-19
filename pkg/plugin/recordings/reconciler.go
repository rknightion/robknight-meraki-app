package recordings

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/robknight/grafana-meraki-plugin/pkg/plugin/alerts"
)

// Reconciliation constants.
//
// These are pulled out as package-level vars (not consts) so reconciler_test
// can crank `reconcileBatchStagger` down to zero — otherwise the
// `apply-in-batches-of-25 with 100ms stagger` spec (mirrors alerts §4.5.13
// R.4) would add ~100ms × batches to every test run.
var (
	// reconcileBatchSize caps concurrent-ish pressure on Grafana's alerting
	// API. A single Reconcile can end up issuing hundreds of writes when a
	// bundle is first installed across many orgs, so we stagger at the
	// batch boundary to stay friendly to the local Grafana instance.
	reconcileBatchSize = 25
	// reconcileBatchStagger is the pause between batches. Tests override.
	reconcileBatchStagger = 100 * time.Millisecond
)

// bundledManagedByLabel is the sentinel label the reconciler writes onto
// every rule it creates AND the filter it applies before considering a
// listed rule a candidate for deletion. Keeping this in one place (not the
// Template YAML) is a safety gate: even if someone hand-authors a rule with
// the `meraki-rec-*` UID prefix, it won't get deleted unless it also carries
// this label AND the kind label below.
//
// bundledRecordingKindLabel is the stronger filter that the recordings
// reconciler applies on top of `managed_by=meraki-plugin`. Alerts and
// recordings share the provisioning endpoint + the `managed_by` label, so
// without this second gate the two reconcilers would delete each other's
// rules on any no-op reconcile. The label gate is non-negotiable.
const (
	bundledManagedByLabel     = "managed_by"
	bundledManagedByValue     = "meraki-plugin"
	bundledRecordingKindLabel = "meraki_kind"
	bundledRecordingKindValue = "recording"
	bundledRecordingUIDPrefix = "meraki-rec-"
)

// DesiredState is the pure input to Reconcile. It intentionally has no
// pointers to the registry or to the Grafana client — the reconciler takes
// those as explicit args so the same DesiredState value can be replayed
// against different backends (real Grafana, test server).
//
// TargetDsUID is the operator-selected Prometheus-compatible datasource UID
// every rendered rule writes samples into. Empty TargetDsUID is a top-level
// precondition failure — the resource handler surfaces this as 412 so the
// UI can prompt the operator to pick a destination (see plan §4.6.4).
type DesiredState struct {
	Groups      map[string]GroupState
	Thresholds  map[string]map[string]map[string]any
	OrgOverride []string
	TargetDsUID string
}

// GroupState is the per-group on/off switch plus per-template on/off state.
// Installed == false means "delete every rule in this group regardless of
// RulesEnabled" — it's the "uninstall the whole group" path.
type GroupState struct {
	Installed    bool
	RulesEnabled map[string]bool
}

// ReconcileResult is the outcome report. UIDs are populated for successes,
// ReconcileFailure carries {UID, Action, Err} for per-call failures.
type ReconcileResult struct {
	Created    []string
	Updated    []string
	Deleted    []string
	Failed     []ReconcileFailure
	StartedAt  time.Time
	FinishedAt time.Time
}

// ReconcileFailure describes one failed API call. The reconciler never
// aborts the whole run on a single failure — it records the error and
// moves on so the happy-path rules still land.
type ReconcileFailure struct {
	UID    string
	Action string
	Err    string
}

// Reconcile is the idempotent entry point. It:
//  1. validates TargetDsUID is set — empty is a top-level precondition
//     failure because every rendered rule needs a write destination,
//  2. resolves the org list (override or via MerakiAPI.GetOrganizations),
//  3. renders every (enabled-group × enabled-template × org) triple through
//     the registry, plumbing TargetDsUID into each Render call,
//  4. lists current bundled rules from Grafana (filtered by folder +
//     managed_by label + meraki_kind=recording label — see the label-gate
//     safety rationale in CLAUDE.md),
//  5. diffs by UID to compute create/update/delete sets (content-diff uses
//     ruleSignature; see its doc comment for why reflect.DeepEqual is wrong),
//  6. applies the diff in batches with a staggered pause.
//
// Per-call errors accumulate in Failed; Reconcile only returns a top-level
// error for preconditions that would poison the whole run (missing target
// datasource, org lookup, folder ensure). Callers should always render both
// result.Failed and err.
func Reconcile(ctx context.Context, g alerts.GrafanaAPI, m alerts.MerakiAPI, r *Registry, desired DesiredState) (ReconcileResult, error) {
	res := ReconcileResult{StartedAt: time.Now()}
	defer func() { res.FinishedAt = time.Now() }()

	if g == nil {
		return res, fmt.Errorf("recordings: Reconcile: GrafanaAPI is nil")
	}
	if r == nil {
		return res, fmt.Errorf("recordings: Reconcile: Registry is nil")
	}
	// Precondition — without a write destination we can't render. Returning
	// a top-level error before ANY writes keeps the 412 surfacing clean: the
	// resource handler maps this to `HTTP 412` and the UI prompts the
	// operator. See plan §4.6.4.
	if desired.TargetDsUID == "" {
		return res, fmt.Errorf("recordings: Reconcile: TargetDsUID is required (no silent fallback to grafana.ini default)")
	}

	// Step 1 — resolve orgs. Override wins; otherwise hit Meraki.
	orgIDs, err := resolveOrgs(ctx, m, desired.OrgOverride)
	if err != nil {
		return res, err
	}

	// Step 2 — render the desired set. Missing templates, bad thresholds,
	// and render errors all get logged as reconciliation failures rather
	// than short-circuiting — other rules can still land.
	desiredRules := map[string]alerts.AlertRule{}
	for groupID, gs := range desired.Groups {
		if !gs.Installed {
			continue
		}
		for templateID, enabled := range gs.RulesEnabled {
			if !enabled {
				continue
			}
			tpl, ok := r.Template(groupID, templateID)
			if !ok {
				res.Failed = append(res.Failed, ReconcileFailure{
					UID:    fmt.Sprintf("meraki-rec-%s-%s-*", groupID, templateID),
					Action: "render",
					Err:    fmt.Sprintf("template %s/%s not in registry", groupID, templateID),
				})
				continue
			}
			overrides := desired.Thresholds[groupID][templateID]
			for _, orgID := range orgIDs {
				rule, rerr := tpl.Render(orgID, overrides, desired.TargetDsUID)
				if rerr != nil {
					res.Failed = append(res.Failed, ReconcileFailure{
						UID:    fmt.Sprintf("meraki-rec-%s-%s-%s", groupID, templateID, orgID),
						Action: "render",
						Err:    rerr.Error(),
					})
					continue
				}
				desiredRules[rule.UID] = rule
			}
		}
	}

	// Step 3 — make sure the folder exists before anything that might POST
	// a rule into it. EnsureFolder is fatal: if we can't make the folder,
	// every subsequent POST will 4xx and there's nothing worth trying.
	if err := g.EnsureFolder(ctx, bundledRecordingsFolderUID, "Meraki (bundled recordings)"); err != nil {
		return res, fmt.Errorf("recordings: ensure folder: %w", err)
	}

	// Step 4 — list existing rules, then apply the two-label gate.
	current, err := g.ListAlertRules(ctx, bundledRecordingsFolderUID)
	if err != nil {
		return res, fmt.Errorf("recordings: list rules: %w", err)
	}
	currentManaged := map[string]alerts.AlertRule{}
	for _, rule := range current {
		if !strings.HasPrefix(rule.UID, bundledRecordingUIDPrefix) {
			continue
		}
		// managed_by gate. Rules without this label are user-owned even if
		// their UID happens to start with meraki-rec-; leave them alone.
		if rule.Labels[bundledManagedByLabel] != bundledManagedByValue {
			continue
		}
		// meraki_kind gate. Without this, an alerts-folder reconcile would
		// treat a recording rule as scope even though alerts and recordings
		// live in distinct folders — the label check is the defence in
		// depth that prevents two reconcilers from ever deleting each
		// other's rules regardless of folder mishap.
		if rule.Labels[bundledRecordingKindLabel] != bundledRecordingKindValue {
			continue
		}
		currentManaged[rule.UID] = rule
	}

	// Step 5 — diff. Deterministic order makes test assertions and logs
	// easier to reason about.
	var creates, updates, deletes []alerts.AlertRule
	var deleteUIDs []string
	for uid, want := range desiredRules {
		got, exists := currentManaged[uid]
		switch {
		case !exists:
			creates = append(creates, want)
		case ruleSignature(got) != ruleSignature(want):
			updates = append(updates, want)
		}
	}
	for uid := range currentManaged {
		if _, stillDesired := desiredRules[uid]; !stillDesired {
			deleteUIDs = append(deleteUIDs, uid)
		}
	}
	sort.Slice(creates, func(i, j int) bool { return creates[i].UID < creates[j].UID })
	sort.Slice(updates, func(i, j int) bool { return updates[i].UID < updates[j].UID })
	sort.Strings(deleteUIDs)
	for _, uid := range deleteUIDs {
		deletes = append(deletes, currentManaged[uid])
	}

	// Step 6 — apply. Batches of reconcileBatchSize with a stagger between
	// batches. Per-call errors don't abort the loop; they accumulate in
	// Failed so the caller can see everything that went wrong in one run.
	apply := func(action string, items []alerts.AlertRule, call func(alerts.AlertRule) error) []string {
		out := make([]string, 0, len(items))
		for i, item := range items {
			if i > 0 && i%reconcileBatchSize == 0 {
				select {
				case <-ctx.Done():
					res.Failed = append(res.Failed, ReconcileFailure{
						UID: item.UID, Action: action, Err: ctx.Err().Error(),
					})
					return out
				case <-time.After(reconcileBatchStagger):
				}
			}
			if err := call(item); err != nil {
				res.Failed = append(res.Failed, ReconcileFailure{
					UID: item.UID, Action: action, Err: err.Error(),
				})
				continue
			}
			out = append(out, item.UID)
		}
		return out
	}

	res.Created = apply("create", creates, func(r alerts.AlertRule) error {
		return g.CreateAlertRule(ctx, r)
	})
	res.Updated = apply("update", updates, func(r alerts.AlertRule) error {
		return g.UpdateAlertRule(ctx, r.UID, r)
	})
	res.Deleted = apply("delete", deletes, func(r alerts.AlertRule) error {
		return g.DeleteAlertRule(ctx, r.UID)
	})

	return res, nil
}

// resolveOrgs returns the effective org-ID list. Explicit OrgOverride beats
// a live Meraki call; an empty override (nil or len=0) falls back to the
// API. Returning an error from here is fatal to the whole Reconcile — if we
// don't know the org set we can't render anything safely.
func resolveOrgs(ctx context.Context, m alerts.MerakiAPI, override []string) ([]string, error) {
	if len(override) > 0 {
		out := make([]string, len(override))
		copy(out, override)
		sort.Strings(out)
		return out, nil
	}
	if m == nil {
		return nil, fmt.Errorf("recordings: MerakiAPI is nil and no OrgOverride provided")
	}
	orgs, err := m.GetOrganizations(ctx)
	if err != nil {
		return nil, fmt.Errorf("recordings: list orgs: %w", err)
	}
	ids := make([]string, 0, len(orgs))
	for _, o := range orgs {
		ids = append(ids, o.ID)
	}
	sort.Strings(ids)
	return ids, nil
}

// ruleSignature returns a canonical JSON string over the fields the reconciler
// actually owns. Two rules with the same signature are byte-identical to the
// plugin's view of the world, which is the diff we want — not the raw
// AlertRule equality.
//
// Why not reflect.DeepEqual? Grafana's GET /alert-rules response includes
// server-assigned fields (ID, Updated, Provenance, computed OrgID). A freshly
// rendered rule has zero values for those, so DeepEqual would report every
// rule as "changed" on every reconcile, producing spurious UPDATE traffic and
// defeating idempotency.
//
// The signature deliberately omits IsPaused — an operator pausing a bundled
// rule in the UI should persist across reconciles, not be flipped back on.
//
// Recording-specific: the Record block is IN the signature. Toggling the
// operator's target-datasource UID is a legitimate content change (samples
// now route to a different Prometheus) and the reconciler must flip every
// rule to an UPDATE when TargetDatasourceUID rotates. Alerts have no Record
// block so the alerts package ignores it; we include it here because it's
// the sole field that captures that rotation.
func ruleSignature(r alerts.AlertRule) string {
	type sig struct {
		UID         string              `json:"uid"`
		Title       string              `json:"title"`
		Data        []alerts.AlertQuery `json:"data"`
		For         string              `json:"for"`
		Annotations map[string]string   `json:"annotations"`
		Labels      map[string]string   `json:"labels"`
		FolderUID   string              `json:"folderUID"`
		RuleGroup   string              `json:"ruleGroup"`
		Record      *alerts.RecordBlock `json:"record"`
	}
	s := sig{
		UID:         r.UID,
		Title:       r.Title,
		Data:        r.Data,
		For:         r.For,
		Annotations: r.Annotations,
		Labels:      r.Labels,
		FolderUID:   r.FolderUID,
		RuleGroup:   r.RuleGroup,
		Record:      r.Record,
	}
	// json.Marshal is deterministic for structs (field order) and sorts map
	// keys, so the resulting byte string is stable across processes.
	b, err := json.Marshal(s)
	if err != nil {
		// Should be unreachable — AlertRule is JSON-safe by construction.
		// Fall back to a format that will never equal a real signature so a
		// bug here surfaces as a spurious UPDATE rather than a silent noop.
		return "sig-err:" + err.Error()
	}
	return string(b)
}
