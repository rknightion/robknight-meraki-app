package alerts

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// update, when set via `go test -update`, rewrites golden fixtures from
// the current Render() output instead of asserting against them. Run
// this, inspect the diff, and commit the updated fixtures.
var update = flag.Bool("update", false, "rewrite golden fixtures for alert templates")

const goldenOrgID = "987654"

// TestGolden walks every template in the registry, renders it with the
// default thresholds and the canonical org ID, and compares against the
// matching testdata fixture. Set `-update` on first run per template.
func TestGolden(t *testing.T) {
	reg, err := LoadRegistry()
	if err != nil {
		t.Fatalf("LoadRegistry: %v", err)
	}
	for _, g := range reg.Groups() {
		for _, tpl := range g.Templates {
			tpl := tpl
			t.Run(g.ID+"/"+tpl.ID, func(t *testing.T) {
				rule, err := tpl.Render(goldenOrgID, nil)
				if err != nil {
					t.Fatalf("Render: %v", err)
				}
				// MarshalIndent (2-space) so diffs on fixtures are small
				// and human-readable.
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
