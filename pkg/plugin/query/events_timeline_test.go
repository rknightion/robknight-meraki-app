package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// TestHandle_NetworkEventsTimeline asserts the aggregator emits a continuous
// ts column plus one int64 field per observed category, with zero-filled
// buckets where nothing happened. Two categories spanning two buckets let us
// verify the bucket floor math.
func TestHandle_NetworkEventsTimeline(t *testing.T) {
	// Panel range: 4h window → 5m buckets (48 buckets). Anchor at a minute
	// boundary so bucket math is obvious.
	from := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	to := from.Add(4 * time.Hour)

	// Place three events: two in the first 5m bucket (one per category), one
	// in the fourth 5m bucket (15-20m) also under "clients".
	payload := fmt.Sprintf(`{"events":[
	  {"occurredAt":%q,"category":"clients","type":"auth","description":"a"},
	  {"occurredAt":%q,"category":"infrastructure","type":"stp","description":"b"},
	  {"occurredAt":%q,"category":"clients","type":"auth","description":"c"}
	],"pageStartAt":null,"pageEndAt":null}`,
		from.Add(1*time.Minute).Format(time.RFC3339),
		from.Add(2*time.Minute).Format(time.RFC3339),
		from.Add(16*time.Minute).Format(time.RFC3339),
	)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/networks/N1/events") {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(srv.Close)

	client, err := meraki.NewClient(meraki.ClientConfig{APIKey: "fake", BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	resp, err := Handle(context.Background(), client, &QueryRequest{
		Range: TimeRange{From: from.UnixMilli(), To: to.UnixMilli()},
		Queries: []MerakiQuery{{
			RefID:        "A",
			Kind:         KindNetworkEventsTimeline,
			OrgID:        "o1",
			NetworkIDs:   []string{"N1"},
			ProductTypes: []string{"wireless"}, // explicit so no fan-out
		}},
	}, Options{})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if got := len(resp.Frames); got != 1 {
		t.Fatalf("got %d frames, want 1", got)
	}

	var frame data.Frame
	if err := json.Unmarshal(resp.Frames[0], &frame); err != nil {
		t.Fatalf("decode frame: %v", err)
	}

	// Expect: ts + two category fields (alphabetically sorted).
	if got := len(frame.Fields); got != 3 {
		t.Fatalf("got %d fields, want 3 (ts + 2 categories); fields=%v", got, frame.Fields)
	}
	if frame.Fields[0].Name != "ts" {
		t.Errorf("first field = %q, want ts", frame.Fields[0].Name)
	}
	if frame.Fields[1].Name != "clients" || frame.Fields[2].Name != "infrastructure" {
		t.Errorf("category order = [%s, %s], want [clients, infrastructure]",
			frame.Fields[1].Name, frame.Fields[2].Name)
	}

	// 4h / 5m = 48 buckets.
	if got := frame.Fields[0].Len(); got != 48 {
		t.Errorf("ts length = %d, want 48", got)
	}

	// Bucket 0 (12:00-12:05): 1 client + 1 infra. Bucket 3 (12:15-12:20): 1 client.
	clients, _ := frame.FieldByName("clients")
	infra, _ := frame.FieldByName("infrastructure")
	want := []struct {
		idx           int
		clients, infra int64
	}{
		{0, 1, 1},
		{1, 0, 0},
		{2, 0, 0},
		{3, 1, 0},
		{47, 0, 0},
	}
	for _, w := range want {
		c, _ := clients.ConcreteAt(w.idx)
		i, _ := infra.ConcreteAt(w.idx)
		if c != w.clients || i != w.infra {
			t.Errorf("bucket %d: clients=%v infra=%v, want clients=%d infra=%d",
				w.idx, c, i, w.clients, w.infra)
		}
	}
}
