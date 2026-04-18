package alerts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeGrafana is an httptest-backed emulator for the Grafana provisioning
// API the reconciler talks to. It records every request (method + path +
// body) and mutates an in-memory store so multi-step scenarios (create
// then list then delete) round-trip correctly.
//
// Deliberately not a full Grafana simulation — it accepts whatever shape
// the client sends and returns it as-is on the next GET. Tests assert on
// the *requests made* (counts by action) rather than the response bodies.
type fakeGrafana struct {
	mu     sync.Mutex
	store  map[string]AlertRule
	calls  []fakeCall
	folder bool // set true once POST /folders or GET /folders/{uid} returns 200
	// overrides: URL path -> status to return instead of the default 2xx.
	// Use to simulate "this one rule fails to POST but the others succeed".
	// The key is "METHOD /path" or "METHOD /path?match=<substring of body>".
	statusFor func(method, path string, body []byte) int
}

type fakeCall struct {
	Method string
	Path   string
	Body   string
}

func newFakeGrafana() *fakeGrafana {
	return &fakeGrafana{store: map[string]AlertRule{}}
}

func (f *fakeGrafana) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = r.Body.Close()

		f.mu.Lock()
		defer f.mu.Unlock()
		f.calls = append(f.calls, fakeCall{Method: r.Method, Path: r.URL.Path, Body: string(body)})

		if f.statusFor != nil {
			if code := f.statusFor(r.Method, r.URL.Path, body); code != 0 {
				w.WriteHeader(code)
				return
			}
		}

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/provisioning/alert-rules":
			out := make([]AlertRule, 0, len(f.store))
			for _, v := range f.store {
				out = append(out, v)
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(out)

		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/provisioning/alert-rules":
			var rule AlertRule
			if err := json.Unmarshal(body, &rule); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			f.store[rule.UID] = rule
			w.WriteHeader(http.StatusCreated)

		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/api/v1/provisioning/alert-rules/"):
			uid := strings.TrimPrefix(r.URL.Path, "/api/v1/provisioning/alert-rules/")
			var rule AlertRule
			if err := json.Unmarshal(body, &rule); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			rule.UID = uid
			f.store[uid] = rule
			w.WriteHeader(http.StatusOK)

		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/api/v1/provisioning/alert-rules/"):
			uid := strings.TrimPrefix(r.URL.Path, "/api/v1/provisioning/alert-rules/")
			delete(f.store, uid)
			w.WriteHeader(http.StatusNoContent)

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/provisioning/folders/"):
			if f.folder {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"uid":"meraki-bundled-folder","title":"Meraki (bundled)"}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)

		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/provisioning/folders":
			f.folder = true
			w.WriteHeader(http.StatusCreated)

		default:
			http.Error(w, "unexpected "+r.Method+" "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// countCalls returns the number of calls matching the given method + path
// prefix. Lets the tests assert e.g. "exactly 1 POST to /alert-rules".
func (f *fakeGrafana) countCalls(method, pathPrefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.Method == method && strings.HasPrefix(c.Path, pathPrefix) {
			n++
		}
	}
	return n
}

// fakeGrafanaAPI adapts fakeGrafana's httptest server into the GrafanaAPI
// interface the reconciler consumes. It's deliberately thin — one tiny HTTP
// client per instance, no retries, no auth beyond a dummy header.
type fakeGrafanaAPI struct {
	base string
	hc   *http.Client
}

func (a fakeGrafanaAPI) do(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer test")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := a.hc.Do(req)
	if err != nil {
		return err
	}
	defer func() { _, _ = io.Copy(io.Discard, resp.Body); _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		buf, _ := io.ReadAll(resp.Body)
		return &httpErr{status: resp.StatusCode, body: string(buf), path: path}
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

type httpErr struct {
	status int
	body   string
	path   string
}

func (e *httpErr) Error() string {
	return e.path + " status " + itoa(e.status) + ": " + e.body
}

// itoa keeps the test file dep-free of strconv for a single integer format.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func (a fakeGrafanaAPI) EnsureFolder(ctx context.Context, uid, title string) error {
	// GET then POST on 404, mirroring the real GrafanaClient.
	err := a.do(ctx, http.MethodGet, "/api/v1/provisioning/folders/"+uid, nil, nil)
	if err == nil {
		return nil
	}
	if he, ok := err.(*httpErr); ok && he.status == http.StatusNotFound {
		return a.do(ctx, http.MethodPost, "/api/v1/provisioning/folders",
			Folder{UID: uid, Title: title}, nil)
	}
	return err
}

func (a fakeGrafanaAPI) ListAlertRules(ctx context.Context, folderUID string) ([]AlertRule, error) {
	var all []AlertRule
	if err := a.do(ctx, http.MethodGet, "/api/v1/provisioning/alert-rules", nil, &all); err != nil {
		return nil, err
	}
	if folderUID == "" {
		return all, nil
	}
	out := make([]AlertRule, 0, len(all))
	for _, r := range all {
		if r.FolderUID == folderUID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (a fakeGrafanaAPI) CreateAlertRule(ctx context.Context, r AlertRule) error {
	return a.do(ctx, http.MethodPost, "/api/v1/provisioning/alert-rules", r, nil)
}

func (a fakeGrafanaAPI) UpdateAlertRule(ctx context.Context, uid string, r AlertRule) error {
	return a.do(ctx, http.MethodPut, "/api/v1/provisioning/alert-rules/"+uid, r, nil)
}

func (a fakeGrafanaAPI) DeleteAlertRule(ctx context.Context, uid string) error {
	return a.do(ctx, http.MethodDelete, "/api/v1/provisioning/alert-rules/"+uid, nil, nil)
}

// stubMeraki is a nil-safe Meraki stub. Returns whatever orgs the test set.
type stubMeraki struct {
	orgs []Organization
	err  error
}

func (s stubMeraki) GetOrganizations(ctx context.Context) ([]Organization, error) {
	return s.orgs, s.err
}

// testReg loads the embedded registry once per test. Phase 1 only ships
// device-offline, so the tests build DesiredState referencing that template.
func testReg(t *testing.T) *Registry {
	t.Helper()
	r, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	return r
}

// testSetup wires a fakeGrafana + fakeGrafanaAPI + neutered batch-stagger
// so tests run in milliseconds. Returns both because most tests need to
// pre-seed the store (fake) and also exercise the reconciler (api).
func testSetup(t *testing.T) (*fakeGrafana, fakeGrafanaAPI) {
	t.Helper()
	// Eliminate the 100ms stagger for tests — scenario 1 exercises only a
	// single batch anyway, but scenario 4 (multiple orgs) could cross the
	// boundary if batch size changes.
	prev := reconcileBatchStagger
	reconcileBatchStagger = 0
	t.Cleanup(func() { reconcileBatchStagger = prev })

	fake := newFakeGrafana()
	srv := fake.server(t)
	api := fakeGrafanaAPI{base: srv.URL, hc: &http.Client{Timeout: 2 * time.Second}}
	return fake, api
}

// deviceOfflineDesired is the default DesiredState for most tests: the
// single Phase 1 template installed in the single group.
func deviceOfflineDesired() DesiredState {
	return DesiredState{
		Groups: map[string]GroupState{
			"availability": {
				Installed:    true,
				RulesEnabled: map[string]bool{"device-offline": true},
			},
		},
	}
}

// renderRule is a convenience wrapper so tests can pre-seed the store with
// canonical rules without re-implementing Render().
func renderRule(t *testing.T, r *Registry, groupID, templateID, orgID string, overrides map[string]any) AlertRule {
	t.Helper()
	tpl, ok := r.Template(groupID, templateID)
	if !ok {
		t.Fatalf("template %s/%s not found", groupID, templateID)
	}
	rule, err := tpl.Render(orgID, overrides)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return rule
}

func TestReconcile_EmptyStartCreatesSingleRule(t *testing.T) {
	fake, api := testSetup(t)
	reg := testReg(t)

	meraki := stubMeraki{orgs: []Organization{{ID: "987654", Name: "Acme"}}}
	res, err := Reconcile(context.Background(), api, meraki, reg, deviceOfflineDesired())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if got := len(res.Created); got != 1 {
		t.Fatalf("Created = %d, want 1 (res=%+v)", got, res)
	}
	if got := fake.countCalls(http.MethodPost, "/api/v1/provisioning/alert-rules"); got != 1 {
		t.Fatalf("POST alert-rules count = %d, want 1", got)
	}
	if got := fake.countCalls(http.MethodPut, "/api/v1/provisioning/alert-rules/"); got != 0 {
		t.Fatalf("PUT count = %d, want 0", got)
	}
	if got := fake.countCalls(http.MethodDelete, "/api/v1/provisioning/alert-rules/"); got != 0 {
		t.Fatalf("DELETE count = %d, want 0", got)
	}
}

func TestReconcile_Idempotent(t *testing.T) {
	fake, api := testSetup(t)
	reg := testReg(t)
	meraki := stubMeraki{orgs: []Organization{{ID: "987654"}}}

	if _, err := Reconcile(context.Background(), api, meraki, reg, deviceOfflineDesired()); err != nil {
		t.Fatalf("Reconcile#1: %v", err)
	}
	// Re-run: nothing should change. Capture counts after run 1 as the
	// baseline so the second run's delta is what we actually assert on.
	postsBefore := fake.countCalls(http.MethodPost, "/api/v1/provisioning/alert-rules")
	putsBefore := fake.countCalls(http.MethodPut, "/api/v1/provisioning/alert-rules/")
	delsBefore := fake.countCalls(http.MethodDelete, "/api/v1/provisioning/alert-rules/")

	res2, err := Reconcile(context.Background(), api, meraki, reg, deviceOfflineDesired())
	if err != nil {
		t.Fatalf("Reconcile#2: %v", err)
	}
	if n := len(res2.Created); n != 0 {
		t.Fatalf("Reconcile#2 Created = %d, want 0 (signature diff leaking)", n)
	}
	if n := len(res2.Updated); n != 0 {
		t.Fatalf("Reconcile#2 Updated = %d, want 0 (signature diff leaking)", n)
	}
	if got := fake.countCalls(http.MethodPost, "/api/v1/provisioning/alert-rules") - postsBefore; got != 0 {
		t.Fatalf("Reconcile#2 extra POST = %d, want 0", got)
	}
	if got := fake.countCalls(http.MethodPut, "/api/v1/provisioning/alert-rules/") - putsBefore; got != 0 {
		t.Fatalf("Reconcile#2 extra PUT = %d, want 0", got)
	}
	if got := fake.countCalls(http.MethodDelete, "/api/v1/provisioning/alert-rules/") - delsBefore; got != 0 {
		t.Fatalf("Reconcile#2 extra DELETE = %d, want 0", got)
	}
}

func TestReconcile_ThresholdChangeProducesUpdate(t *testing.T) {
	fake, api := testSetup(t)
	reg := testReg(t)

	// Pre-seed the store with the DEFAULT rendered rule (for_duration=5m).
	def := renderRule(t, reg, "availability", "device-offline", "987654", nil)
	fake.store[def.UID] = def
	fake.folder = true // pretend the folder already exists

	// Now run with an override changing for_duration. Signature must differ,
	// so we expect exactly 1 UPDATE, 0 CREATE, 0 DELETE.
	desired := deviceOfflineDesired()
	desired.Thresholds = map[string]map[string]map[string]any{
		"availability": {
			"device-offline": {"for_duration": "15m"},
		},
	}
	res, err := Reconcile(context.Background(), api,
		stubMeraki{orgs: []Organization{{ID: "987654"}}}, reg, desired)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n := len(res.Updated); n != 1 {
		t.Fatalf("Updated = %d, want 1 (failed=%+v)", n, res.Failed)
	}
	if n := len(res.Created); n != 0 {
		t.Fatalf("Created = %d, want 0", n)
	}
	if n := len(res.Deleted); n != 0 {
		t.Fatalf("Deleted = %d, want 0", n)
	}
}

func TestReconcile_GroupDisabledDeletes(t *testing.T) {
	fake, api := testSetup(t)
	reg := testReg(t)

	def := renderRule(t, reg, "availability", "device-offline", "987654", nil)
	fake.store[def.UID] = def
	fake.folder = true

	desired := deviceOfflineDesired()
	desired.Groups["availability"] = GroupState{Installed: false,
		RulesEnabled: map[string]bool{"device-offline": true}}

	res, err := Reconcile(context.Background(), api,
		stubMeraki{orgs: []Organization{{ID: "987654"}}}, reg, desired)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n := len(res.Deleted); n != 1 {
		t.Fatalf("Deleted = %d, want 1 (failed=%+v)", n, res.Failed)
	}
	if n := len(res.Created)+len(res.Updated); n != 0 {
		t.Fatalf("Created+Updated = %d, want 0", n)
	}
}

func TestReconcile_NewOrgFanOut(t *testing.T) {
	fake, api := testSetup(t)
	reg := testReg(t)

	// Store already has org1's rule; reconciler should create org2's only.
	org1 := renderRule(t, reg, "availability", "device-offline", "111", nil)
	fake.store[org1.UID] = org1
	fake.folder = true

	res, err := Reconcile(context.Background(), api,
		stubMeraki{orgs: []Organization{{ID: "111"}, {ID: "222"}}}, reg, deviceOfflineDesired())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n := len(res.Created); n != 1 {
		t.Fatalf("Created = %d, want 1 (got UIDs=%v)", n, res.Created)
	}
	if !strings.Contains(res.Created[0], "-222") {
		t.Fatalf("expected create for org 222, got %q", res.Created[0])
	}
	if n := len(res.Updated)+len(res.Deleted); n != 0 {
		t.Fatalf("Updated+Deleted = %d, want 0", n)
	}
}

func TestReconcile_OrgRemovedDeletesStraggler(t *testing.T) {
	fake, api := testSetup(t)
	reg := testReg(t)

	org1 := renderRule(t, reg, "availability", "device-offline", "111", nil)
	org2 := renderRule(t, reg, "availability", "device-offline", "222", nil)
	fake.store[org1.UID] = org1
	fake.store[org2.UID] = org2
	fake.folder = true

	// Meraki now only knows about org1, so org2's rule is a straggler.
	res, err := Reconcile(context.Background(), api,
		stubMeraki{orgs: []Organization{{ID: "111"}}}, reg, deviceOfflineDesired())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n := len(res.Deleted); n != 1 {
		t.Fatalf("Deleted = %d, want 1", n)
	}
	if !strings.Contains(res.Deleted[0], "-222") {
		t.Fatalf("expected delete for org 222, got %q", res.Deleted[0])
	}
	if n := len(res.Created)+len(res.Updated); n != 0 {
		t.Fatalf("Created+Updated = %d, want 0", n)
	}
}

func TestReconcile_PartialFailureDoesNotAbort(t *testing.T) {
	fake, api := testSetup(t)
	reg := testReg(t)

	// Empty store, two orgs → two creates planned. Force the POST that
	// carries org 222's UID to return 500; org 111's POST still succeeds.
	fake.statusFor = func(method, path string, body []byte) int {
		if method == http.MethodPost && path == "/api/v1/provisioning/alert-rules" &&
			strings.Contains(string(body), "meraki-availability-device-offline-222") {
			return http.StatusInternalServerError
		}
		return 0
	}

	res, err := Reconcile(context.Background(), api,
		stubMeraki{orgs: []Organization{{ID: "111"}, {ID: "222"}}}, reg, deviceOfflineDesired())
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n := len(res.Created); n != 1 {
		t.Fatalf("Created = %d, want 1 (res=%+v)", n, res)
	}
	if n := len(res.Failed); n != 1 {
		t.Fatalf("Failed = %d, want 1 (res=%+v)", n, res)
	}
	if res.Failed[0].Action != "create" {
		t.Fatalf("Failed[0].Action = %q, want create", res.Failed[0].Action)
	}
	if !strings.Contains(res.Failed[0].UID, "-222") {
		t.Fatalf("Failed[0].UID = %q, want to contain -222", res.Failed[0].UID)
	}
}

func TestReconcile_ManagedByLabelGate(t *testing.T) {
	fake, api := testSetup(t)
	reg := testReg(t)

	// Seed a rule that LOOKS like one of ours (meraki- UID prefix) but
	// DOESN'T carry the managed_by label. The reconciler must leave it
	// alone even when desired state is empty.
	poser := AlertRule{
		UID:       "meraki-availability-device-offline-000",
		Title:     "User's own rule",
		FolderUID: bundledFolderUID,
		Labels:    map[string]string{"severity": "warning"}, // no managed_by!
	}
	fake.store[poser.UID] = poser
	fake.folder = true

	// Empty desired state: every bundled rule should be deleted… EXCEPT
	// the poser, which lacks the label gate.
	empty := DesiredState{Groups: map[string]GroupState{}}

	res, err := Reconcile(context.Background(), api,
		stubMeraki{orgs: []Organization{{ID: "000"}}}, reg, empty)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if n := len(res.Deleted); n != 0 {
		t.Fatalf("Deleted = %d, want 0 (label gate failed — would have wiped user rule)", n)
	}
	// And it's still in the store.
	if _, ok := fake.store[poser.UID]; !ok {
		t.Fatalf("poser rule was deleted despite missing managed_by label")
	}
}

func TestReconcile_ContentDiffStable(t *testing.T) {
	// Unit test for ruleSignature: a rule mutated in non-owned fields (OrgID
	// filled by Grafana on GET, IsPaused flipped by user) should NOT look
	// like a pending update.
	r1 := AlertRule{
		UID:          "x",
		Title:        "t",
		Condition:    "C",
		For:          "5m",
		NoDataState:  "NoData",
		ExecErrState: "Error",
		Labels:       map[string]string{"a": "1"},
		Annotations:  map[string]string{"b": "2"},
	}
	r2 := r1
	r2.OrgID = 1    // server fills this in on GET
	r2.IsPaused = true // user paused in UI — we don't want to flip it back

	if ruleSignature(r1) != ruleSignature(r2) {
		t.Fatalf("signature changed across non-owned fields; got:\n%s\n%s",
			ruleSignature(r1), ruleSignature(r2))
	}

	// Sanity: a real content change DOES produce a different signature.
	r3 := r1
	r3.For = "15m"
	if ruleSignature(r1) == ruleSignature(r3) {
		t.Fatalf("signature failed to detect For change")
	}
}
