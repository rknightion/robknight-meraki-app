package plugin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

// stubProber implements GrafanaProber for CheckHealth unit tests — lets the
// three alerts-bundle states (ready / toggle-off / unreachable) be exercised
// without spinning up a live Grafana.
type stubProber struct {
	ready  bool
	reason string
	err    error
}

func (s stubProber) Probe(_ context.Context) (bool, string, error) {
	return s.ready, s.reason, s.err
}

// newAppForAlertsProbe wires an *App whose Meraki client points at the given
// httptest server (mirrors newAppWithClient in resources_test.go) AND whose
// GrafanaProber factory is replaced with a stub. Used by the alerts-bundle
// tests below.
func newAppForAlertsProbe(t *testing.T, merakiSrv string, proberFactory func(*backend.GrafanaCfg) (GrafanaProber, error)) *App {
	t.Helper()
	app := newAppWithClient(t, merakiSrv)
	app.newGrafanaProber = proberFactory
	return app
}

// merakiHandlerOK emits a single org + an identity payload — the happy path
// for Meraki connectivity so the alerts-bundle probe is the interesting
// signal under test.
func merakiHandlerOK() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/organizations"):
			_, _ = w.Write([]byte(`[{"id":"o1","name":"Primary"}]`))
		case strings.Contains(r.URL.Path, "/administered/identities/me"):
			_, _ = w.Write([]byte(`{"name":"Rob","email":"rob@example.com"}`))
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
		}
	})
}

func TestCheckHealth_AlertsBundle_Ready(t *testing.T) {
	srv := httptest.NewServer(merakiHandlerOK())
	t.Cleanup(srv.Close)
	app := newAppForAlertsProbe(t, srv.URL, func(*backend.GrafanaCfg) (GrafanaProber, error) {
		return stubProber{ready: true, reason: "ready"}, nil
	})

	res, err := app.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatalf("CheckHealth: %v", err)
	}
	if res.Status != backend.HealthStatusOk {
		t.Fatalf("status = %v, want Ok", res.Status)
	}
	if !strings.Contains(res.Message, "Alerts bundle: ready.") {
		t.Fatalf("message missing alerts bundle ready marker; got %q", res.Message)
	}
}

func TestCheckHealth_AlertsBundle_ToggleOff(t *testing.T) {
	srv := httptest.NewServer(merakiHandlerOK())
	t.Cleanup(srv.Close)
	app := newAppForAlertsProbe(t, srv.URL, func(*backend.GrafanaCfg) (GrafanaProber, error) {
		return stubProber{
			ready:  false,
			reason: "unavailable — enable externalServiceAccounts feature toggle or upgrade Grafana build",
		}, nil
	})

	res, err := app.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatalf("CheckHealth: %v", err)
	}
	// Meraki probe is the authoritative pass/fail — alerts-bundle issues must
	// NOT degrade status.
	if res.Status != backend.HealthStatusOk {
		t.Fatalf("status = %v, want Ok (alerts-bundle unavailability must not fail health)", res.Status)
	}
	if !strings.Contains(res.Message, "externalServiceAccounts") {
		t.Fatalf("message should surface toggle name; got %q", res.Message)
	}
	if !strings.Contains(res.Message, "Alerts bundle: unavailable") {
		t.Fatalf("message missing alerts bundle unavailable marker; got %q", res.Message)
	}
}

func TestCheckHealth_AlertsBundle_Unreachable(t *testing.T) {
	srv := httptest.NewServer(merakiHandlerOK())
	t.Cleanup(srv.Close)
	// Prober factory itself errors — simulates missing plugin-app client
	// secret or AppURL. CheckHealth degrades gracefully.
	app := newAppForAlertsProbe(t, srv.URL, func(*backend.GrafanaCfg) (GrafanaProber, error) {
		return nil, errors.New("grafana config missing")
	})

	res, err := app.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatalf("CheckHealth: %v", err)
	}
	if res.Status != backend.HealthStatusOk {
		t.Fatalf("status = %v, want Ok", res.Status)
	}
	if !strings.Contains(res.Message, "Alerts bundle: unavailable") {
		t.Fatalf("message missing alerts bundle unavailable marker; got %q", res.Message)
	}
}

// TestCheckHealth_AlertsBundle_TransportError confirms transport-level
// probe failures (network error) degrade to "could not reach Grafana alert
// provisioning API" rather than surfacing the raw error.
func TestCheckHealth_AlertsBundle_TransportError(t *testing.T) {
	srv := httptest.NewServer(merakiHandlerOK())
	t.Cleanup(srv.Close)
	app := newAppForAlertsProbe(t, srv.URL, func(*backend.GrafanaCfg) (GrafanaProber, error) {
		return stubProber{err: errors.New("dial tcp: refused")}, nil
	})

	res, err := app.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatalf("CheckHealth: %v", err)
	}
	if res.Status != backend.HealthStatusOk {
		t.Fatalf("status = %v, want Ok", res.Status)
	}
	if !strings.Contains(res.Message, "could not reach Grafana alert provisioning API") {
		t.Fatalf("message missing transport-error marker; got %q", res.Message)
	}
}
