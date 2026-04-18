package query

import "net/url"

// productTypeRoute maps a Meraki productType to the `ROUTES` slug that owns
// that device family's detail page on the frontend. Values must match
// `src/constants.ts::ROUTES` exactly. Unknown product types fall back to the
// sensor route — the original MT scene is tolerant of non-MT serials and
// degrades to an empty "latest readings" panel.
func productTypeRoute(productType string) string {
	switch productType {
	case "wireless":
		return "access-points"
	case "switch":
		return "switches"
	case "appliance":
		return "appliances"
	case "camera":
		return "cameras"
	case "cellularGateway":
		return "cellular-gateways"
	case "sensor":
		return "sensors"
	default:
		return "sensors"
	}
}

// deviceDrilldownURL composes the full `/a/<plugin>/<family-route>/<serial>`
// URL for a device of the given productType. prefix is the full plugin base
// path (threaded through Options.PluginPathPrefix by the dispatcher) — e.g.
// `/a/rknightion-meraki-app`. Returns "" when prefix is empty so handlers can
// safely elide the column when the dispatcher hasn't set it.
func deviceDrilldownURL(prefix, productType, serial string) string {
	if prefix == "" || serial == "" {
		return ""
	}
	return prefix + "/" + productTypeRoute(productType) + "/" + url.PathEscape(serial)
}
