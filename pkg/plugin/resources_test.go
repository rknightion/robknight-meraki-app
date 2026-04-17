package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
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
