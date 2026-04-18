package alerts

// InMemoryGrafana is an e2e-test-only stub that satisfies GrafanaAPI without
// talking to a real Grafana instance. Wired in from pkg/plugin/app.go when
// E2E_MOCK_GRAFANA=1 is set on the plugin process, so Playwright specs can
// exercise the full /alerts/* handler surface (including Reconcile →
// create/update/delete) against an in-memory map instead of spoofing
// Grafana's own provisioning endpoints.
//
// Intentionally NOT build-tagged — the file adds no production dependencies
// and the env-var guard in app.go gates activation. Keeping it in the normal
// build also means `go test ./pkg/...` can use the same stub for reconciler
// exercising without a duplicate copy.

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// InMemoryGrafana records every CRUD call into in-memory maps keyed by UID
// (rules) and folder UID (folders). All methods take the same arguments as
// the real GrafanaClient, so swapping the implementation at the App factory
// boundary is zero-friction.
type InMemoryGrafana struct {
	mu      sync.Mutex
	rules   map[string]AlertRule
	folders map[string]Folder
}

// NewInMemoryGrafana constructs an empty in-memory stub. Every App instance
// that opts into E2E_MOCK_GRAFANA shares a single stub for the lifetime of
// the process so repeated /alerts/reconcile calls behave like talking to one
// Grafana instance across the whole test session.
func NewInMemoryGrafana() *InMemoryGrafana {
	return &InMemoryGrafana{
		rules:   map[string]AlertRule{},
		folders: map[string]Folder{},
	}
}

// EnsureFolder matches the real client's idempotent contract — create on
// absent, no-op on present. Title overwrites are fine because the real
// endpoint also PUTs title on second-call.
func (g *InMemoryGrafana) EnsureFolder(_ context.Context, uid, title string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.folders[uid] = Folder{UID: uid, Title: title}
	return nil
}

// ListAlertRules returns every rule whose FolderUID matches, matching
// GrafanaClient.ListAlertRules's post-filter behaviour. An empty folderUID
// returns every rule in the stub (useful for diagnostic dumps in tests).
// Results are sorted by UID for deterministic assertions.
func (g *InMemoryGrafana) ListAlertRules(_ context.Context, folderUID string) ([]AlertRule, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]AlertRule, 0, len(g.rules))
	for _, r := range g.rules {
		if folderUID == "" || r.FolderUID == folderUID {
			out = append(out, r)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UID < out[j].UID })
	return out, nil
}

// CreateAlertRule errors on UID collision so the reconciler's idempotency
// story (create-only-if-missing, update-otherwise) is actually exercised —
// hiding the collision would let a regression where diff-classification
// mis-tags a rule silently succeed.
func (g *InMemoryGrafana) CreateAlertRule(_ context.Context, r AlertRule) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.rules[r.UID]; exists {
		return fmt.Errorf("e2e mock: create of existing rule %q", r.UID)
	}
	g.rules[r.UID] = r
	return nil
}

// UpdateAlertRule errors on missing-UID for the symmetrical reason as Create.
func (g *InMemoryGrafana) UpdateAlertRule(_ context.Context, uid string, r AlertRule) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.rules[uid]; !exists {
		return fmt.Errorf("e2e mock: update of missing rule %q", uid)
	}
	g.rules[uid] = r
	return nil
}

// DeleteAlertRule errors on missing UID so tests surface accidental
// double-deletes; the real Grafana client returns 404 here too.
func (g *InMemoryGrafana) DeleteAlertRule(_ context.Context, uid string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, exists := g.rules[uid]; !exists {
		return fmt.Errorf("e2e mock: delete of missing rule %q", uid)
	}
	delete(g.rules, uid)
	return nil
}

// Reset wipes the in-memory store. Tests that share a process (e.g. Playwright
// workers serialised against one Grafana container) can call this between
// specs via a dedicated /alerts/e2e-reset endpoint if we need per-test
// isolation — not currently wired.
func (g *InMemoryGrafana) Reset() {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.rules = map[string]AlertRule{}
	g.folders = map[string]Folder{}
}
