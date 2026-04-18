package meraki

// ClientVersion is the semver string embedded in the User-Agent header of every
// Meraki API request this plugin makes. It is reported back to Meraki's API
// Analytics dashboard, letting operators see how much traffic this plugin
// generates against their organization.
//
// Bump this alongside package.json and CHANGELOG.md for every release. Meraki
// expects an ApplicationName/Version VendorName triple per
// https://developer.cisco.com/meraki/api-v1/user-agents-overview/ — the
// forward slash is the reserved version separator; the space is the
// application/vendor separator. Application and vendor names must omit
// spaces, hyphens, and special characters, which is why we use
// "GrafanaMerakiPlugin" (not "Grafana-Meraki-Plugin") and "rknightion"
// (not "rknightion-meraki").
const ClientVersion = "0.4.0"

// UserAgentApplication is the ApplicationName half of the User-Agent pair.
// It identifies this plugin in Meraki's API Analytics dashboard and MUST NOT
// contain spaces, hyphens, or special characters (forward slashes are reserved
// for the version suffix).
const UserAgentApplication = "GrafanaMerakiPlugin"

// UserAgentVendor is the VendorName half of the User-Agent pair. Matches the
// current plugin-ID namespace; flip to "robknight" when the Q.7 rename lands.
const UserAgentVendor = "rknightion"

// BuildUserAgent assembles the spec-compliant User-Agent string:
//
//	"GrafanaMerakiPlugin/<ClientVersion> rknightion"
//
// Callers should prefer this helper over string-concatenating the constants
// inline, so any future format tweak (vendor rename, sub-component tag) lands
// in one place.
func BuildUserAgent() string {
	return UserAgentApplication + "/" + ClientVersion + " " + UserAgentVendor
}
