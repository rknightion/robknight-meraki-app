package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

// Reconciliation constants.
//
// These are pulled out as package-level vars (not consts) so reconciler_test
// can crank `reconcileBatchStagger` down to zero — otherwise the
// `apply-in-batches-of-25 with 100ms stagger` spec (todos §4.5.13 R.4)
// would add ~100ms × batches to every test run.
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
// the `meraki-*` UID prefix, it won't get deleted unless it also carries
// this label.
const (
	bundledManagedByLabel = "managed_by"
	bundledManagedByValue = "meraki-plugin"
)

// GrafanaAPI is the narrow surface the reconciler needs from the Grafana
// provisioning client. Declaring it here (rather than importing the concrete
// plugin.GrafanaClient) keeps the alerts package free of a cyclic dep on the
// outer plugin package.
type GrafanaAPI interface {
	EnsureFolder(ctx context.Context, uid, title string) error
	ListAlertRules(ctx context.Context, folderUID string) ([]AlertRule, error)
	CreateAlertRule(ctx context.Context, r AlertRule) error
	UpdateAlertRule(ctx context.Context, uid string, r AlertRule) error
	DeleteAlertRule(ctx context.Context, uid string) error
}

// MerakiAPI is the subset of the Meraki client the reconciler depends on.
// Kept minimal so the test harness can stub it with a struct literal.
type MerakiAPI interface {
	GetOrganizations(ctx context.Context) ([]Organization, error)
}

// Organization is the reconciler's view of a Meraki org. Only ID is
// load-bearing (it feeds Template.Render); Name is kept around for future
// telemetry / log output.
type Organization struct {
	ID   string
	Name string
}

// DesiredState is the pure input to Reconcile. It intentionally has no
// pointers to the registry or to the Grafana client — the reconciler takes
// those as explicit args so the same DesiredState value can be replayed
// against different backends (real Grafana, test server).
type DesiredState struct {
	Groups      map[string]GroupState
	Thresholds  map[string]map[string]map[string]any
	OrgOverride []string
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
//  1. resolves the org list (override or via MerakiAPI.GetOrganizations),
//  2. renders every (enabled-group × enabled-template × org) triple through
//     the registry,
//  3. lists current bundled rules from Grafana (filtered by folder +
//     managed_by label — see the label-gate safety rationale in CLAUDE.md),
//  4. diffs by UID to compute create/update/delete sets (content-diff uses
//     ruleSignature; see its doc comment for why reflect.DeepEqual is wrong),
//  5. applies the diff in batches with a staggered pause.
//
// Per-call errors accumulate in Failed; Reconcile only returns a top-level
// error for preconditions that would poison the whole run (org lookup, folder
// ensure). Callers should always render both result.Failed and err.
func Reconcile(ctx context.Context, g GrafanaAPI, m MerakiAPI, r *Registry, desired DesiredState) (ReconcileResult, error) {
	res := ReconcileResult{StartedAt: time.Now()}
	defer func() { res.FinishedAt = time.Now() }()

	if g == nil {
		return res, fmt.Errorf("alerts: Reconcile: GrafanaAPI is nil")
	}
	if r == nil {
		return res, fmt.Errorf("alerts: Reconcile: Registry is nil")
	}

	// Step 1 — resolve orgs. Override wins; otherwise hit Meraki.
	orgIDs, err := resolveOrgs(ctx, m, desired.OrgOverride)
	if err != nil {
		return res, err
	}

	// Step 2 — render the desired set. Missing templates, bad thresholds,
	// and render errors all get logged as reconciliation failures rather
	// than short-circuiting — other rules can still land.
	desiredRules := map[string]AlertRule{}
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
					UID:    fmt.Sprintf("meraki-%s-%s-*", groupID, templateID),
					Action: "render",
					Err:    fmt.Sprintf("template %s/%s not in registry", groupID, templateID),
				})
				continue
			}
			overrides := desired.Thresholds[groupID][templateID]
			for _, orgID := range orgIDs {
				rule, rerr := tpl.Render(orgID, overrides)
				if rerr != nil {
					res.Failed = append(res.Failed, ReconcileFailure{
						UID:    fmt.Sprintf("meraki-%s-%s-%s", groupID, templateID, orgID),
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
	if err := g.EnsureFolder(ctx, bundledFolderUID, "Meraki (bundled)"); err != nil {
		return res, fmt.Errorf("alerts: ensure folder: %w", err)
	}

	// Step 4 — list existing rules, then apply the label-gate.
	current, err := g.ListAlertRules(ctx, bundledFolderUID)
	if err != nil {
		return res, fmt.Errorf("alerts: list rules: %w", err)
	}
	currentManaged := map[string]AlertRule{}
	for _, rule := range current {
		if !strings.HasPrefix(rule.UID, "meraki-") {
			continue
		}
		// managed_by gate. Rules without this label are user-owned even if
		// their UID happens to start with meraki-; leave them alone.
		if rule.Labels[bundledManagedByLabel] != bundledManagedByValue {
			continue
		}
		currentManaged[rule.UID] = rule
	}

	// Step 5 — diff. Deterministic order makes test assertions and logs
	// easier to reason about.
	var creates, updates, deletes []AlertRule
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
	apply := func(action string, items []AlertRule, call func(AlertRule) error) []string {
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

	res.Created = apply("create", creates, func(r AlertRule) error {
		return g.CreateAlertRule(ctx, r)
	})
	res.Updated = apply("update", updates, func(r AlertRule) error {
		return g.UpdateAlertRule(ctx, r.UID, r)
	})
	res.Deleted = apply("delete", deletes, func(r AlertRule) error {
		return g.DeleteAlertRule(ctx, r.UID)
	})

	return res, nil
}

// resolveOrgs returns the effective org-ID list. Explicit OrgOverride beats
// a live Meraki call; an empty override (nil or len=0) falls back to the
// API. Returning an error from here is fatal to the whole Reconcile — if we
// don't know the org set we can't render anything safely.
func resolveOrgs(ctx context.Context, m MerakiAPI, override []string) ([]string, error) {
	if len(override) > 0 {
		out := make([]string, len(override))
		copy(out, override)
		sort.Strings(out)
		return out, nil
	}
	if m == nil {
		return nil, fmt.Errorf("alerts: MerakiAPI is nil and no OrgOverride provided")
	}
	orgs, err := m.GetOrganizations(ctx)
	if err != nil {
		return nil, fmt.Errorf("alerts: list orgs: %w", err)
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
// The signature deliberately omits IsPaused — a user pausing a bundled rule
// in the UI should persist across reconciles, not be flipped back on.
func ruleSignature(r AlertRule) string {
	type sig struct {
		UID          string            `json:"uid"`
		Title        string            `json:"title"`
		Condition    string            `json:"condition"`
		Data         []AlertQuery      `json:"data"`
		NoDataState  string            `json:"noDataState"`
		ExecErrState string            `json:"execErrState"`
		For          string            `json:"for"`
		Annotations  map[string]string `json:"annotations"`
		Labels       map[string]string `json:"labels"`
		FolderUID    string            `json:"folderUID"`
		RuleGroup    string            `json:"ruleGroup"`
	}
	s := sig{
		UID:          r.UID,
		Title:        r.Title,
		Condition:    r.Condition,
		Data:         r.Data,
		NoDataState:  r.NoDataState,
		ExecErrState: r.ExecErrState,
		For:          r.For,
		Annotations:  r.Annotations,
		Labels:       r.Labels,
		FolderUID:    r.FolderUID,
		RuleGroup:    r.RuleGroup,
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
