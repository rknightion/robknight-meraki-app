package recordings

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// update, when set via `go test -update`, rewrites golden fixtures from
// the current Render() output instead of asserting against them. Run
// this on first-time template authoring, inspect the diff, and commit
// the updated fixtures.
var update = flag.Bool("update", false, "rewrite golden fixtures for recording templates")

// goldenOrgID is the canonical org ID used for every fixture. Kept in
// sync with the alerts package so the two suites share the same
// substitution surface and fixture diffs stay human-legible.
const goldenOrgID = "987654"

// goldenTargetDsUID is the canonical target-DS UID used for every
// fixture. Render() refuses an empty target so we supply a stable
// placeholder — tests for the "empty targetDsUID" guard live in
// registry_test.go.
const goldenTargetDsUID = "golden-prom-uid"

// TestGolden walks every recording template, renders it with the default
// thresholds + canonical org ID + canonical target-DS UID, and compares
// against the matching testdata fixture. Run `-update` on first-time
// template authoring to generate the fixture.
//
// This is the canary that any accidental change to rendering (YAML
// shape, Record block injection, UID format, for="0s" enforcement,
// label backfill) surfaces here before reaching Grafana.
func TestGolden(t *testing.T) {
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	for _, g := range reg.Groups() {
		for _, tpl := range g.Templates {
			tpl := tpl
			t.Run(g.ID+"/"+tpl.ID, func(t *testing.T) {
				rule, err := tpl.Render(goldenOrgID, nil, goldenTargetDsUID)
				if err != nil {
					t.Fatalf("Render: %v", err)
				}
				got, err := json.MarshalIndent(rule, "", "  ")
				if err != nil {
					t.Fatalf("Marshal: %v", err)
				}
				got = append(got, '\n')

				path := filepath.Join("testdata", g.ID+"-"+tpl.ID+"-"+goldenOrgID+".golden.json")

				if *update {
					if err := os.MkdirAll("testdata", 0o755); err != nil {
						t.Fatalf("MkdirAll: %v", err)
					}
					if err := os.WriteFile(path, got, 0o644); err != nil {
						t.Fatalf("WriteFile: %v", err)
					}
					t.Logf("wrote fixture %s", path)
					return
				}

				want, err := os.ReadFile(path)
				if err != nil {
					t.Fatalf("read golden %s: %v (rerun with -update to create)", path, err)
				}
				if string(got) != string(want) {
					t.Errorf("golden mismatch for %s\n--- got\n%s\n--- want\n%s", path, got, want)
				}
			})
		}
	}
}
