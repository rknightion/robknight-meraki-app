package meraki_test

import (
	"testing"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// TestSwitchPortStatusOptions_SerialsParamName guards against the Meraki
// `statuses/bySwitch` endpoint contract for array filters. Verified against
// api.meraki.com 2026-04-18:
//
//   - `serials[]=X` (bracketed) — accepted, filters correctly.
//   - `serials=X` (plain) — HTTP 400 "'serials' must be an array".
//
// If someone changes the values() builder back to the plain form, per-switch
// detail panels silently go empty (the 400 gets captured as a frame notice
// but the UI just shows "No port status reported" — easy to miss).
func TestSwitchPortStatusOptions_SerialsParamName(t *testing.T) {
	v := meraki.SwitchPortStatusOptionsValues(meraki.SwitchPortStatusOptions{
		Serials:    []string{"Q2BX-Q43Y-RR5C"},
		NetworkIDs: []string{"N_1"},
	})
	if got := v["serials[]"]; len(got) != 1 || got[0] != "Q2BX-Q43Y-RR5C" {
		t.Errorf("serials[] param = %v; want [Q2BX-Q43Y-RR5C]", got)
	}
	if got := v["serials"]; len(got) != 0 {
		t.Errorf("unbracketed `serials` must NOT be set (endpoint returns 400); got %v", got)
	}
	if got := v["networkIds[]"]; len(got) != 1 || got[0] != "N_1" {
		t.Errorf("networkIds[] param = %v; want [N_1]", got)
	}
	if got := v["networkIds"]; len(got) != 0 {
		t.Errorf("unbracketed `networkIds` must NOT be set; got %v", got)
	}
}
