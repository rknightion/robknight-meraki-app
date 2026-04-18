package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
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
