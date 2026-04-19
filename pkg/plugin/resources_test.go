package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
	"github.com/robknight/grafana-meraki-plugin/pkg/plugin/alerts"
	"github.com/robknight/grafana-meraki-plugin/pkg/plugin/recordings"
)

type mockCallResourceResponseSender struct {
	response *backend.CallResourceResponse
}

func (s *mockCallResourceResponseSender) Send(r *backend.CallResourceResponse) error {
	s.response = r
	return nil
}

func TestHandlePing(t *testing.T) {
	inst, err := NewApp(context.Background(), backend.AppInstanceSettings{})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	app, ok := inst.(*App)
	if !ok {
		t.Fatalf("instance is not *App: %T", inst)
	}

	sender := &mockCallResourceResponseSender{}
	if err := app.CallResource(context.Background(), &backend.CallResourceRequest{
		Method: http.MethodGet,
		Path:   "ping",
	}, sender); err != nil {
		t.Fatalf("CallResource: %v", err)
	}
	if sender.response == nil {
		t.Fatal("no response")
	}
	if sender.response.Status != http.StatusOK {
		t.Fatalf("status: got %d, want %d", sender.response.Status, http.StatusOK)
	}
	var body struct {
		Message    string `json:"message"`
		Configured bool   `json:"configured"`
	}
	if err := json.NewDecoder(bytes.NewReader(sender.response.Body)).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Message != "ok" {
		t.Fatalf("message: got %q, want %q", body.Message, "ok")
	}
	if body.Configured {
		t.Fatal("configured: expected false with empty settings")
	}
}

func TestCheckHealthUnconfigured(t *testing.T) {
	inst, err := NewApp(context.Background(), backend.AppInstanceSettings{})
	if err != nil {
		t.Fatalf("NewApp: %v", err)
	}
	app := inst.(*App)
	res, err := app.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatalf("CheckHealth: %v", err)
	}
	if res.Status != backend.HealthStatusError {
		t.Fatalf("status: got %v, want %v", res.Status, backend.HealthStatusError)
	}
	if res.Message == "" {
		t.Fatal("expected non-empty error message")
	}
}

// newAppWithClient assembles an *App whose meraki.Client points at the given
// httptest server — avoiding the NewApp factory path which requires a real
// base URL. Used by the CheckHealth tests below to stub /organizations and
// /administered/identities/me responses. Logger is the SDK's default so
// non-fatal debug logs don't nil-panic.
func newAppWithClient(t *testing.T, baseURL string) *App {
	t.Helper()
	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: baseURL})
	if err != nil {
		t.Fatalf("meraki.NewClient: %v", err)
	}
	return &App{client: client, logger: log.DefaultLogger}
}

// TestCheckHealth_IncludesIdentity verifies both the identity probe result
// flows into the Message ("Connected to Meraki as <email>") and the
// JSONDetails payload (email + name + organizationCount).
func TestCheckHealth_IncludesIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/organizations"):
			_, _ = w.Write([]byte(`[{"id":"o1","name":"Primary"}]`))
		case strings.Contains(r.URL.Path, "/administered/identities/me"):
			_, _ = w.Write([]byte(`{"name":"Rob Knight","email":"rob@example.com","authentication":{"mode":"email","twoFactor":{"enabled":true}}}`))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	app := newAppWithClient(t, srv.URL)
	res, err := app.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatalf("CheckHealth: %v", err)
	}
	if res.Status != backend.HealthStatusOk {
		t.Fatalf("status = %v, want Ok", res.Status)
	}
	if !strings.Contains(res.Message, "rob@example.com") {
		t.Fatalf("message missing email; got %q", res.Message)
	}
	if !strings.Contains(res.Message, "1 organization") {
		t.Fatalf("message missing organization count; got %q", res.Message)
	}
	var details struct {
		Email             string `json:"email"`
		Name              string `json:"name"`
		TwoFactorEnabled  bool   `json:"twoFactorEnabled"`
		OrganizationCount int    `json:"organizationCount"`
	}
	if err := json.Unmarshal(res.JSONDetails, &details); err != nil {
		t.Fatalf("JSONDetails decode: %v (raw=%s)", err, res.JSONDetails)
	}
	if details.Email != "rob@example.com" {
		t.Fatalf("details.Email = %q, want rob@example.com", details.Email)
	}
	if details.Name != "Rob Knight" {
		t.Fatalf("details.Name = %q, want Rob Knight", details.Name)
	}
	if !details.TwoFactorEnabled {
		t.Fatalf("details.TwoFactorEnabled = false, want true")
	}
	if details.OrganizationCount != 1 {
		t.Fatalf("details.OrganizationCount = %d, want 1", details.OrganizationCount)
	}
}

// TestCheckHealth_FallsBackWhenIdentityFails verifies CheckHealth still
// returns OK when the identity probe fails — the organizations probe is the
// authoritative health signal.
func TestCheckHealth_FallsBackWhenIdentityFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/organizations"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"o1","name":"Primary"}]`))
		case strings.Contains(r.URL.Path, "/administered/identities/me"):
			http.Error(w, "internal", http.StatusInternalServerError)
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	app := newAppWithClient(t, srv.URL)
	res, err := app.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatalf("CheckHealth: %v", err)
	}
	if res.Status != backend.HealthStatusOk {
		t.Fatalf("status = %v, want Ok (identity failure should not fail health)", res.Status)
	}
	// Fallback message — no email because the identity probe failed.
	if strings.Contains(res.Message, " as ") {
		t.Fatalf("message should not include ' as ' when identity probe failed; got %q", res.Message)
	}
	// JSONDetails still populated with organizationCount, but no email/name.
	var details struct {
		Email             string `json:"email"`
		OrganizationCount int    `json:"organizationCount"`
	}
	if err := json.Unmarshal(res.JSONDetails, &details); err != nil {
		t.Fatalf("JSONDetails decode: %v", err)
	}
	if details.Email != "" {
		t.Fatalf("details.Email = %q, want empty", details.Email)
	}
	if details.OrganizationCount != 1 {
		t.Fatalf("details.OrganizationCount = %d, want 1", details.OrganizationCount)
	}
}

func TestLoadSettings(t *testing.T) {
	s := backend.AppInstanceSettings{
		JSONData: []byte(`{"baseUrl":"https://api.meraki.cn/api/v1","sharedFraction":0.5,"isApiKeySet":true}`),
		DecryptedSecureJSONData: map[string]string{"merakiApiKey": "abc123"},
	}
	got, err := loadSettings(s)
	if err != nil {
		t.Fatalf("loadSettings: %v", err)
	}
	if got.BaseURL != "https://api.meraki.cn/api/v1" {
		t.Errorf("BaseURL: got %q", got.BaseURL)
	}
	if got.SharedFraction != 0.5 {
		t.Errorf("SharedFraction: got %v", got.SharedFraction)
	}
	if got.APIKey != "abc123" {
		t.Errorf("APIKey: got %q", got.APIKey)
	}
	if !got.IsApiKeySet {
		t.Error("IsApiKeySet: got false, want true")
	}
}

// --- /alerts/* handler tests ----------------------------------------------
//
// These tests exercise the Phase 3 resource endpoints in isolation. To keep
// them fast + hermetic they use:
//
//   - An in-memory fakeGrafanaAPI (below) that satisfies alerts.GrafanaAPI
//     without network I/O. The real GrafanaClient is tested separately in
//     grafanaclient_test.go.
//   - newAppForAlerts helper that builds an App with a working registry and
//     an injected fake for newGrafanaAPI so the status + reconcile +
//     uninstall paths don't need a live Grafana.
//   - t.TempDir() + GF_PATHS_DATA override so the alertsStore writes under
//     the test's scratch dir, not the user's real Grafana data path.

// fakeGrafanaAPI is the test double for alerts.GrafanaAPI. It keeps the
// stored rules in a map keyed by UID, letting tests pre-seed state and then
// observe the effect of a reconcile. All methods are concurrency-safe so
// the reconciler's batch-goroutines don't race — even though the Phase 1
// reconciler is sequential, future concurrency work shouldn't break these
// tests just because the mutex is absent.
type fakeGrafanaAPI struct {
	mu      sync.Mutex
	rules   map[string]alerts.AlertRule
	folder  bool
	listErr error // when non-nil, ListAlertRules returns this instead of the map
}

func newFakeGrafanaAPI() *fakeGrafanaAPI {
	return &fakeGrafanaAPI{rules: map[string]alerts.AlertRule{}}
}

func (f *fakeGrafanaAPI) EnsureFolder(ctx context.Context, uid, title string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.folder = true
	return nil
}

func (f *fakeGrafanaAPI) ListAlertRules(ctx context.Context, folderUID string) ([]alerts.AlertRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.listErr != nil {
		return nil, f.listErr
	}
	out := make([]alerts.AlertRule, 0, len(f.rules))
	for _, r := range f.rules {
		if folderUID == "" || r.FolderUID == folderUID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeGrafanaAPI) CreateAlertRule(ctx context.Context, r alerts.AlertRule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rules[r.UID] = r
	return nil
}

func (f *fakeGrafanaAPI) UpdateAlertRule(ctx context.Context, uid string, r alerts.AlertRule) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r.UID = uid
	f.rules[uid] = r
	return nil
}

func (f *fakeGrafanaAPI) DeleteAlertRule(ctx context.Context, uid string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.rules, uid)
	return nil
}

// newAppForAlerts builds an App preloaded with the embedded alerts registry,
// a scratch-dir alertsStore, and an injected GrafanaAPI fake. It does NOT
// attach a Meraki client — tests that need Configured()==true wire one up
// explicitly (mirror of newAppWithClient).
func newAppForAlerts(t *testing.T, api alerts.GrafanaAPI) *App {
	t.Helper()
	t.Setenv("GF_PATHS_DATA", t.TempDir())
	reg, err := alerts.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	store, err := newAlertsStore(alertsDataDir())
	if err != nil {
		t.Fatalf("newAlertsStore: %v", err)
	}
	return &App{
		logger:         log.DefaultLogger,
		alertsRegistry: reg,
		alertsStore:    store,
		newGrafanaAPI: func(*backend.GrafanaCfg) (alerts.GrafanaAPI, error) {
			return api, nil
		},
	}
}

// callResource drives the plugin's resource mux end-to-end via a real
// http.ServeMux (not CallResource -> httpadapter) so the test can inspect
// the HTTP status + JSON body directly. Mirrors what Grafana does at runtime
// minus the gRPC hop.
func callResource(t *testing.T, app *App, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	app.registerRoutes(mux)
	var r *http.Request
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		r = httptest.NewRequest(method, path, bytes.NewReader(buf))
		r.Header.Set("Content-Type", "application/json")
	} else {
		r = httptest.NewRequest(method, path, nil)
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, r)
	return rr
}

// TestHandleAlertsTemplates_ReturnsRegistry verifies the static templates
// endpoint emits the Phase 1 group + template (availability / device-offline)
// and that no Configured() gate blocks it.
func TestHandleAlertsTemplates_ReturnsRegistry(t *testing.T) {
	app := newAppForAlerts(t, newFakeGrafanaAPI())
	rr := callResource(t, app, http.MethodGet, "/alerts/templates", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var body alertsTemplatesResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Groups) < 1 {
		t.Fatalf("groups = %d, want >= 1", len(body.Groups))
	}
	var avail *alertGroupDTO
	for i := range body.Groups {
		if body.Groups[i].ID == "availability" {
			avail = &body.Groups[i]
		}
	}
	if avail == nil {
		t.Fatalf("availability group missing; got %+v", body.Groups)
	}
	// device-offline is the Phase 1 seed. §4.5.7 added further templates
	// across multiple groups; assert device-offline is still present
	// under availability without pinning the full list (that's the job of
	// pkg/plugin/alerts/registry_test.go).
	found := false
	for _, tpl := range avail.Templates {
		if tpl.ID == "device-offline" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("availability templates = %+v, want device-offline present", avail.Templates)
	}
}

// TestHandleAlertsStatus_EmptyStore verifies /alerts/status returns 200 with
// an empty installed list and grafanaReady=true when no rules have been
// reconciled yet but Grafana is reachable.
func TestHandleAlertsStatus_EmptyStore(t *testing.T) {
	app := newAppForAlerts(t, newFakeGrafanaAPI())
	rr := callResource(t, app, http.MethodGet, "/alerts/status", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var body alertsStatusResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Installed) != 0 {
		t.Fatalf("installed = %+v, want empty", body.Installed)
	}
	if !body.GrafanaReady {
		t.Fatal("grafanaReady = false, want true (fake API succeeds)")
	}
	if body.LastReconciledAt != nil {
		t.Fatalf("lastReconciledAt = %v, want nil", body.LastReconciledAt)
	}
}

// TestHandleAlertsStatus_WithManagedRule seeds a fake rule and confirms the
// status handler emits it with GroupID/TemplateID/OrgID parsed out of the
// UID.
func TestHandleAlertsStatus_WithManagedRule(t *testing.T) {
	api := newFakeGrafanaAPI()
	api.rules["meraki-availability-device-offline-987654"] = alerts.AlertRule{
		UID:       "meraki-availability-device-offline-987654",
		Title:     "Device offline",
		FolderUID: bundledFolderUID,
		Labels:    map[string]string{"managed_by": "meraki-plugin"},
	}
	app := newAppForAlerts(t, api)
	rr := callResource(t, app, http.MethodGet, "/alerts/status", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body alertsStatusResponse
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if len(body.Installed) != 1 {
		t.Fatalf("installed = %+v, want 1 rule", body.Installed)
	}
	got := body.Installed[0]
	if got.GroupID != "availability" || got.TemplateID != "device-offline" || got.OrgID != "987654" {
		t.Fatalf("parsed UID fields wrong: %+v", got)
	}
	if !got.Enabled {
		t.Fatal("enabled = false, want true (IsPaused=false)")
	}
}

// TestHandleAlertsStatus_SkipsUnmanagedRules confirms the managed_by label
// gate: a rule with a meraki- UID prefix but no managed_by label is NOT
// included in the status output.
func TestHandleAlertsStatus_SkipsUnmanagedRules(t *testing.T) {
	api := newFakeGrafanaAPI()
	api.rules["meraki-user-owned-123"] = alerts.AlertRule{
		UID:       "meraki-user-owned-123",
		FolderUID: bundledFolderUID,
		Labels:    map[string]string{"severity": "info"}, // no managed_by
	}
	app := newAppForAlerts(t, api)
	rr := callResource(t, app, http.MethodGet, "/alerts/status", nil)
	var body alertsStatusResponse
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if len(body.Installed) != 0 {
		t.Fatalf("installed = %+v, want empty (unmanaged rule leaked through gate)", body.Installed)
	}
}

// newAppForReconcile wires the full set of dependencies needed by
// /alerts/reconcile: alerts registry, store, GrafanaAPI fake, AND a working
// Meraki client pointed at an httptest server emitting one org.
func newAppForReconcile(t *testing.T, api alerts.GrafanaAPI) (*App, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/organizations"):
			_, _ = w.Write([]byte(`[{"id":"987654","name":"Acme"}]`))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	t.Setenv("GF_PATHS_DATA", t.TempDir())
	reg, err := alerts.LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	store, err := newAlertsStore(alertsDataDir())
	if err != nil {
		t.Fatalf("newAlertsStore: %v", err)
	}
	mc, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("meraki.NewClient: %v", err)
	}
	app := &App{
		logger:         log.DefaultLogger,
		client:         mc,
		alertsRegistry: reg,
		alertsStore:    store,
		newGrafanaAPI: func(*backend.GrafanaCfg) (alerts.GrafanaAPI, error) {
			return api, nil
		},
	}
	return app, srv
}

// TestHandleAlertsReconcile_RequiresConfigured verifies the 412 path when
// no Meraki API key is set. The reconciler fans out to Meraki to resolve
// the org list, so without a configured client the whole call must short-
// circuit.
func TestHandleAlertsReconcile_RequiresConfigured(t *testing.T) {
	app := newAppForAlerts(t, newFakeGrafanaAPI()) // no client attached
	rr := callResource(t, app, http.MethodPost, "/alerts/reconcile", desiredStateDTO{})
	if rr.Code != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, want 412 (body=%s)", rr.Code, rr.Body.String())
	}
}

// TestHandleAlertsReconcile_CreatesRule exercises the happy path: POST a
// DesiredState with the device-offline template enabled, verify the fake
// Grafana ended up with a POST'd rule, and that the summary was persisted.
func TestHandleAlertsReconcile_CreatesRule(t *testing.T) {
	api := newFakeGrafanaAPI()
	app, _ := newAppForReconcile(t, api)

	body := desiredStateDTO{
		Groups: map[string]groupStateDTO{
			"availability": {Installed: true, RulesEnabled: map[string]bool{"device-offline": true}},
		},
	}
	rr := callResource(t, app, http.MethodPost, "/alerts/reconcile", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var result alerts.ReconcileResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Created) != 1 {
		t.Fatalf("Created = %+v, want 1 UID", result.Created)
	}
	wantUID := "meraki-availability-device-offline-987654"
	if result.Created[0] != wantUID {
		t.Fatalf("Created[0] = %q, want %q", result.Created[0], wantUID)
	}
	// Fake Grafana should now hold the rule.
	if _, ok := api.rules[wantUID]; !ok {
		t.Fatalf("rule %q missing from fake store after reconcile (rules=%+v)", wantUID, api.rules)
	}
	// Summary persisted.
	st := app.alertsStore.Get()
	if st.LastReconciledAt.IsZero() {
		t.Fatal("lastReconciledAt not persisted")
	}
	if st.LastReconcileSummary.Created != 1 {
		t.Fatalf("summary.Created = %d, want 1", st.LastReconcileSummary.Created)
	}
}

// TestHandleAlertsUninstallAll_DeletesManaged seeds one managed rule + one
// unmanaged rule, POSTs uninstall-all, and verifies ONLY the managed rule
// is deleted (label gate preserved) AND that Configured() is not required.
func TestHandleAlertsUninstallAll_DeletesManaged(t *testing.T) {
	api := newFakeGrafanaAPI()
	managedUID := "meraki-availability-device-offline-987654"
	api.rules[managedUID] = alerts.AlertRule{
		UID:       managedUID,
		FolderUID: bundledFolderUID,
		Labels:    map[string]string{"managed_by": "meraki-plugin"},
	}
	posterUID := "meraki-user-owned-000"
	api.rules[posterUID] = alerts.AlertRule{
		UID:       posterUID,
		FolderUID: bundledFolderUID,
		Labels:    map[string]string{"severity": "info"}, // no managed_by
	}

	app := newAppForAlerts(t, api) // no Meraki client — uninstall shouldn't need one

	rr := callResource(t, app, http.MethodPost, "/alerts/uninstall-all", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var result alerts.ReconcileResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Deleted) != 1 {
		t.Fatalf("Deleted = %+v, want 1 UID", result.Deleted)
	}
	if result.Deleted[0] != managedUID {
		t.Fatalf("Deleted[0] = %q, want %q", result.Deleted[0], managedUID)
	}
	// Managed rule gone, unmanaged rule still present.
	if _, ok := api.rules[managedUID]; ok {
		t.Fatal("managed rule still present after uninstall-all")
	}
	if _, ok := api.rules[posterUID]; !ok {
		t.Fatal("unmanaged rule was deleted — label gate failed")
	}
	// Summary persisted.
	st := app.alertsStore.Get()
	if st.LastReconcileSummary.Deleted != 1 {
		t.Fatalf("summary.Deleted = %d, want 1", st.LastReconcileSummary.Deleted)
	}
}

// TestParseRuleUID covers a handful of edge cases for the UID parser that
// the status handler depends on — template IDs can contain hyphens (e.g.
// `device-offline`) so we walk from both ends.
func TestParseRuleUID(t *testing.T) {
	tests := []struct{ uid, g, tpl, org string }{
		{"meraki-availability-device-offline-987654", "availability", "device-offline", "987654"},
		{"meraki-wan-uplink-down-111", "wan", "uplink-down", "111"},
		{"meraki-a-b-c", "a", "b", "c"},
		{"not-a-meraki-rule", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.uid, func(t *testing.T) {
			g, tpl, org := parseRuleUID(tc.uid)
			if g != tc.g || tpl != tc.tpl || org != tc.org {
				t.Fatalf("parseRuleUID(%q) = (%q,%q,%q), want (%q,%q,%q)",
					tc.uid, g, tpl, org, tc.g, tc.tpl, tc.org)
			}
		})
	}
}

// TestParseRecordingRuleUID exercises the recordings-variant UID parser
// which strips the `meraki-rec-` prefix before walking from both ends.
func TestParseRecordingRuleUID(t *testing.T) {
	tests := []struct{ uid, g, tpl, org string }{
		{"meraki-rec-availability-device-status-overview-987654", "availability", "device-status-overview", "987654"},
		{"meraki-rec-wan-appliance-uplink-status-111", "wan", "appliance-uplink-status", "111"},
		{"meraki-rec-a-b-c", "a", "b", "c"},
		// Alert UIDs must NOT parse as recording UIDs (prefix guard).
		{"meraki-availability-device-offline-987654", "", "", ""},
		{"not-a-meraki-rule", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.uid, func(t *testing.T) {
			g, tpl, org := parseRecordingRuleUID(tc.uid)
			if g != tc.g || tpl != tc.tpl || org != tc.org {
				t.Fatalf("parseRecordingRuleUID(%q) = (%q,%q,%q), want (%q,%q,%q)",
					tc.uid, g, tpl, org, tc.g, tc.tpl, tc.org)
			}
		})
	}
}

// --- /recordings/* handler tests ------------------------------------------

// newAppForRecordings mirrors newAppForAlerts — preloads the embedded
// recordings registry, a scratch-dir recordingsStore, and an injected
// GrafanaAPI fake. Does NOT attach a Meraki client; reconcile tests that
// need Configured()==true wire one via newAppForRecordingsReconcile.
func newAppForRecordings(t *testing.T, api alerts.GrafanaAPI) *App {
	t.Helper()
	t.Setenv("GF_PATHS_DATA", t.TempDir())
	reg, err := recordings.LoadRegistry()
	if err != nil {
		t.Fatalf("recordings.LoadRegistry: %v", err)
	}
	store, err := newRecordingsStore(alertsDataDir())
	if err != nil {
		t.Fatalf("newRecordingsStore: %v", err)
	}
	return &App{
		logger:             log.DefaultLogger,
		recordingsRegistry: reg,
		recordingsStore:    store,
		newGrafanaAPI: func(*backend.GrafanaCfg) (alerts.GrafanaAPI, error) {
			return api, nil
		},
	}
}

// newAppForRecordingsReconcile wires everything needed by
// /recordings/reconcile: registry, store, GrafanaAPI fake, Meraki client
// pointed at an httptest server, and the operator-picked target DS UID.
// Tests override the target UID on app.settings directly so the gate
// path is exercised cleanly.
func newAppForRecordingsReconcile(t *testing.T, api alerts.GrafanaAPI, targetDsUID string) (*App, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/organizations"):
			_, _ = w.Write([]byte(`[{"id":"987654","name":"Acme"}]`))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	app := newAppForRecordings(t, api)
	app.settings.RecordingsTargetDatasourceUID = targetDsUID
	mc, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("meraki.NewClient: %v", err)
	}
	app.client = mc
	return app, srv
}

// TestHandleRecordingsTemplates_ReturnsRegistry smoke-tests that the
// embedded YAML templates are surfaced and every template carries a
// metric name — the UI needs that metadata to render without re-hitting
// the backend.
func TestHandleRecordingsTemplates_ReturnsRegistry(t *testing.T) {
	app := newAppForRecordings(t, newFakeGrafanaAPI())
	rr := callResource(t, app, http.MethodGet, "/recordings/templates", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var body recordingsTemplatesResponse
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Groups) < 1 {
		t.Fatalf("groups = %d, want >= 1", len(body.Groups))
	}
	var avail *recordingGroupDTO
	for i := range body.Groups {
		if body.Groups[i].ID == "availability" {
			avail = &body.Groups[i]
		}
	}
	if avail == nil {
		t.Fatalf("availability group missing; got %+v", body.Groups)
	}
	var dso *recordingTemplateDTO
	for i := range avail.Templates {
		if avail.Templates[i].ID == "device-status-overview" {
			dso = &avail.Templates[i]
		}
	}
	if dso == nil {
		t.Fatalf("device-status-overview template missing")
	}
	if dso.Metric != "meraki_device_status_count" {
		t.Fatalf("metric = %q, want meraki_device_status_count", dso.Metric)
	}
}

// TestHandleRecordingsStatus_FilterRequiresKindLabel seeds four rules
// that each differ in exactly one label or prefix, and asserts the
// status handler's combined filter (UID prefix + managed_by +
// meraki_kind) only surfaces the fully-conforming rule. This is the
// defence-in-depth gate that keeps alert rules from being misclassified
// as recordings (and vice versa).
func TestHandleRecordingsStatus_FilterRequiresKindLabel(t *testing.T) {
	api := newFakeGrafanaAPI()
	folder := recordings.BundledRecordingsFolderUID()

	// (1) Fully valid — must surface.
	valid := "meraki-rec-availability-device-status-overview-987654"
	api.rules[valid] = alerts.AlertRule{
		UID: valid, FolderUID: folder,
		Labels: map[string]string{"managed_by": "meraki-plugin", "meraki_kind": "recording"},
	}
	// (2) Missing meraki_kind — should be filtered.
	missingKind := "meraki-rec-wan-appliance-uplink-status-111"
	api.rules[missingKind] = alerts.AlertRule{
		UID: missingKind, FolderUID: folder,
		Labels: map[string]string{"managed_by": "meraki-plugin"},
	}
	// (3) Alert UID prefix mistakenly labelled as recording — should be filtered.
	wrongPrefix := "meraki-alerts-device-offline-222"
	api.rules[wrongPrefix] = alerts.AlertRule{
		UID: wrongPrefix, FolderUID: folder,
		Labels: map[string]string{"managed_by": "meraki-plugin", "meraki_kind": "recording"},
	}
	// (4) User-authored rule sharing the rec prefix but no managed_by.
	userOwned := "meraki-rec-user-thing-333"
	api.rules[userOwned] = alerts.AlertRule{
		UID: userOwned, FolderUID: folder,
		Labels: map[string]string{"meraki_kind": "recording"},
	}

	app := newAppForRecordings(t, api)
	rr := callResource(t, app, http.MethodGet, "/recordings/status", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body recordingsStatusResponse
	_ = json.NewDecoder(rr.Body).Decode(&body)
	if len(body.Installed) != 1 {
		t.Fatalf("installed = %+v, want exactly 1 (the fully-conforming rule)", body.Installed)
	}
	if body.Installed[0].UID != valid {
		t.Fatalf("installed UID = %q, want %q", body.Installed[0].UID, valid)
	}
	if body.Installed[0].GroupID != "availability" || body.Installed[0].TemplateID != "device-status-overview" {
		t.Fatalf("parsed UID fields wrong: %+v", body.Installed[0])
	}
}

// TestHandleRecordingsReconcile_RequiresTargetDS verifies the 412 gate:
// an empty settings.RecordingsTargetDatasourceUID + no override in the
// request body must fail precondition. This is the critical plan
// invariant — no silent fallback to grafana.ini defaults.
func TestHandleRecordingsReconcile_RequiresTargetDS(t *testing.T) {
	api := newFakeGrafanaAPI()
	app, _ := newAppForRecordingsReconcile(t, api, "") // empty target DS
	body := recordingsDesiredStateDTO{
		Groups: map[string]groupStateDTO{
			"availability": {Installed: true, RulesEnabled: map[string]bool{"device-status-overview": true}},
		},
	}
	rr := callResource(t, app, http.MethodPost, "/recordings/reconcile", body)
	if rr.Code != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, want 412 (body=%s)", rr.Code, rr.Body.String())
	}
	if len(api.rules) != 0 {
		t.Fatalf("rules were written despite 412: %+v", api.rules)
	}
}

// TestHandleRecordingsReconcile_CreatesRule exercises the happy path:
// target DS configured, one template enabled, org resolved via the
// fake Meraki server. Verifies (a) the rule lands in the fake Grafana
// store, (b) its Record block carries the operator's target UID, (c)
// the reconcile summary gets persisted.
func TestHandleRecordingsReconcile_CreatesRule(t *testing.T) {
	api := newFakeGrafanaAPI()
	app, _ := newAppForRecordingsReconcile(t, api, "my-prometheus-uid")

	body := recordingsDesiredStateDTO{
		Groups: map[string]groupStateDTO{
			"availability": {Installed: true, RulesEnabled: map[string]bool{"device-status-overview": true}},
		},
	}
	rr := callResource(t, app, http.MethodPost, "/recordings/reconcile", body)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var result recordings.ReconcileResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Created) != 1 {
		t.Fatalf("Created = %+v, want 1 UID", result.Created)
	}
	wantUID := "meraki-rec-availability-device-status-overview-987654"
	if result.Created[0] != wantUID {
		t.Fatalf("Created[0] = %q, want %q", result.Created[0], wantUID)
	}
	rule, ok := api.rules[wantUID]
	if !ok {
		t.Fatalf("rule %q missing from fake store after reconcile", wantUID)
	}
	if rule.Record == nil {
		t.Fatal("rule.Record is nil — recording-rule payload must carry a Record block")
	}
	if rule.Record.TargetDatasourceUID != "my-prometheus-uid" {
		t.Fatalf("Record.TargetDatasourceUID = %q, want my-prometheus-uid", rule.Record.TargetDatasourceUID)
	}
	st := app.recordingsStore.Get()
	if st.LastReconciledAt.IsZero() {
		t.Fatal("lastReconciledAt not persisted")
	}
	if st.LastReconcileSummary.Created != 1 {
		t.Fatalf("summary.Created = %d, want 1", st.LastReconcileSummary.Created)
	}
}

// TestHandleRecordingsUninstallAll_DeletesManagedRules seeds a mix of
// rules and verifies uninstall-all deletes only those carrying BOTH the
// managed_by and meraki_kind=recording labels, leaving user-authored and
// alert-bundle rules untouched. Does NOT require Configured() — uninstall
// path never calls Meraki.
func TestHandleRecordingsUninstallAll_DeletesManagedRules(t *testing.T) {
	api := newFakeGrafanaAPI()
	folder := recordings.BundledRecordingsFolderUID()
	managedUID := "meraki-rec-availability-device-status-overview-987654"
	api.rules[managedUID] = alerts.AlertRule{
		UID: managedUID, FolderUID: folder,
		Labels: map[string]string{"managed_by": "meraki-plugin", "meraki_kind": "recording"},
	}
	userUID := "meraki-rec-user-custom-metric-123"
	api.rules[userUID] = alerts.AlertRule{
		UID: userUID, FolderUID: folder,
		Labels: map[string]string{"meraki_kind": "recording"}, // no managed_by
	}

	app := newAppForRecordings(t, api) // no Meraki client, no target DS

	rr := callResource(t, app, http.MethodPost, "/recordings/uninstall-all", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rr.Code, rr.Body.String())
	}
	var result recordings.ReconcileResult
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(result.Deleted) != 1 {
		t.Fatalf("Deleted = %+v, want 1 UID", result.Deleted)
	}
	if result.Deleted[0] != managedUID {
		t.Fatalf("Deleted[0] = %q, want %q", result.Deleted[0], managedUID)
	}
	if _, ok := api.rules[managedUID]; ok {
		t.Fatal("managed recording rule still present after uninstall-all")
	}
	if _, ok := api.rules[userUID]; !ok {
		t.Fatal("user-authored rule was deleted — label gate failed")
	}
}

