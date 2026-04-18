package query

import "testing"

// TestDeviceDrilldownURL is a table-driven check of the productType → route
// mapping plus the two short-circuit branches (empty prefix, empty serial,
// unknown product types falling back to sensor). Keeping the mapping pinned
// here guards against silent renames when `ROUTES` changes on the frontend.
func TestDeviceDrilldownURL(t *testing.T) {
	const prefix = "/a/robknight-meraki-app"

	cases := []struct {
		name        string
		prefix      string
		productType string
		serial      string
		want        string
	}{
		{
			name:        "wireless maps to access-points",
			prefix:      prefix,
			productType: "wireless",
			serial:      "Q2AA-AAAA-AAAA",
			want:        "/a/robknight-meraki-app/access-points/Q2AA-AAAA-AAAA",
		},
		{
			name:        "switch maps to switches",
			prefix:      prefix,
			productType: "switch",
			serial:      "SW-1",
			want:        "/a/robknight-meraki-app/switches/SW-1",
		},
		{
			name:        "appliance maps to appliances",
			prefix:      prefix,
			productType: "appliance",
			serial:      "MX-1",
			want:        "/a/robknight-meraki-app/appliances/MX-1",
		},
		{
			name:        "camera maps to cameras",
			prefix:      prefix,
			productType: "camera",
			serial:      "Q2MV-AAAA-AAAA",
			want:        "/a/robknight-meraki-app/cameras/Q2MV-AAAA-AAAA",
		},
		{
			name:        "cellularGateway maps to cellular-gateways",
			prefix:      prefix,
			productType: "cellularGateway",
			serial:      "Q2MG-AAAA-AAAA",
			want:        "/a/robknight-meraki-app/cellular-gateways/Q2MG-AAAA-AAAA",
		},
		{
			name:        "sensor maps to sensors",
			prefix:      prefix,
			productType: "sensor",
			serial:      "Q2MT-AAAA-AAAA",
			want:        "/a/robknight-meraki-app/sensors/Q2MT-AAAA-AAAA",
		},
		{
			name:        "unknown productType falls back to sensor route",
			prefix:      prefix,
			productType: "wirelessController",
			serial:      "S1",
			want:        "/a/robknight-meraki-app/sensors/S1",
		},
		{
			name:        "empty productType falls back to sensor route",
			prefix:      prefix,
			productType: "",
			serial:      "S1",
			want:        "/a/robknight-meraki-app/sensors/S1",
		},
		{
			name:        "empty prefix short-circuits to empty string",
			prefix:      "",
			productType: "wireless",
			serial:      "S1",
			want:        "",
		},
		{
			name:        "empty serial short-circuits to empty string",
			prefix:      prefix,
			productType: "wireless",
			serial:      "",
			want:        "",
		},
		{
			name:        "serial with special characters is URL-escaped",
			prefix:      prefix,
			productType: "sensor",
			serial:      "a/b c",
			want:        "/a/robknight-meraki-app/sensors/a%2Fb%20c",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := deviceDrilldownURL(tc.prefix, tc.productType, tc.serial)
			if got != tc.want {
				t.Fatalf("deviceDrilldownURL(%q, %q, %q) = %q, want %q",
					tc.prefix, tc.productType, tc.serial, got, tc.want)
			}
		})
	}
}
