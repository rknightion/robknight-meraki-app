// Package meraki — firmware lifecycle endpoints (v0.5 §4.4.4-B).
//
// This file wraps the four upstream endpoints the Firmware & Lifecycle page
// needs:
//
//   - GET /organizations/{organizationId}/firmware/upgrades
//     Org-wide list of past + scheduled upgrade events. One row per
//     network-product upgrade. Status is one of "started", "completed",
//     "canceled", "failed", "skipped", "scheduled" (per Meraki docs as of
//     2026-04). Link-paginated.
//
//   - GET /organizations/{organizationId}/firmware/upgrades/byDevice
//     Per-device upgrade status (currently only MS + MR). Link-paginated.
//     Each row has serial, name, network, current version, available next
//     version, and the most recent + pending upgrade entries
//     (scheduledFor, status, fromVersion, toVersion, completedAt).
//
//   - GET /organizations/{organizationId}/inventory/devices?eoxStatuses[]=…
//     EOL/EOS source decision (B.P preconditions step 1):
//
//     **Meraki DOES expose EOL information via the API.** The inventory
//     endpoint accepts an `eoxStatuses[]` filter and the per-row response
//     carries `eoxStatus` (string), `endOfSaleDate`, and
//     `endOfSupportDate` ISO-8601 fields. We use the API path — no
//     hand-maintained table is needed. Verified via ctx7 against
//     `/openapi/api_meraki_api_v1_openapispec` on 2026-04-18.
//
//     The handler computes `daysUntil` (until end-of-support) at frame-
//     emit time so the EOL table can sort/colour by urgency.
//
//   - GET /networks/{networkId}/firmwareUpgrades is a sibling endpoint that
//     exposes per-network upgrade-window settings (dayOfWeek, hourOfDay,
//     timezone). It is NOT wrapped here — the org-level
//     /firmware/upgrades feed already carries scheduledFor on each event,
//     and adding a per-network fan-out would multiply round-trips by N
//     networks for marginal extra information. A future iteration can add
//     it if operators ask for the upgrade-window cron literal in the UI.
//
// All three endpoints fall under the per-org rate limiter and share the
// same 15 m TTL — firmware state is slow-moving (Meraki publishes new
// versions on the order of weeks, EOL announcements on the order of
// months).
//
// Endpoint URLs + parameters captured on 2026-04-18 via:
//
//	npx ctx7@latest docs /openapi/api_meraki_api_v1_openapispec \
//	  "firmware upgrades pending byDevice end-of-life"

package meraki

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// FirmwareUpgrade is one row from
// `GET /organizations/{organizationId}/firmware/upgrades`. Each entry
// describes a firmware change applied (or scheduled) to one product type
// inside one network. Times are RFC3339 strings on the wire; we decode into
// `*time.Time` so the JSON `null` cases (the field is absent for upgrades
// that have not yet completed) don't introduce false zero-times.
type FirmwareUpgrade struct {
	UpgradeID      string                 `json:"upgradeId,omitempty"`
	UpgradeBatchID string                 `json:"upgradeBatchId,omitempty"`
	Time           *time.Time             `json:"time,omitempty"`
	Status         string                 `json:"status,omitempty"`
	ProductType    string                 `json:"productType,omitempty"`
	Network        FirmwareUpgradeNetwork `json:"network"`
	FromVersion    FirmwareVersionRef     `json:"fromVersion"`
	ToVersion      FirmwareVersionRef     `json:"toVersion"`
	Staged         bool                   `json:"staged,omitempty"`
}

// FirmwareUpgradeNetwork is the compact network reference embedded on each
// upgrade entry. Mirrors the wire shape; `name` is optional on some upgrade
// types so we leave it omitempty.
type FirmwareUpgradeNetwork struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

// FirmwareVersionRef is `{id, shortName, firmware, releaseType, releaseDate}`
// on the wire. We surface the human-friendly `shortName` since that's what
// the Meraki dashboard renders ("MR 30.7" rather than the opaque numeric id).
type FirmwareVersionRef struct {
	ID          string     `json:"id,omitempty"`
	ShortName   string     `json:"shortName,omitempty"`
	Firmware    string     `json:"firmware,omitempty"`
	ReleaseType string     `json:"releaseType,omitempty"`
	ReleaseDate *time.Time `json:"releaseDate,omitempty"`
}

// FirmwareUpgradeOptions filters the org-level firmware/upgrades feed.
// Each field is optional; empty values are not emitted.
type FirmwareUpgradeOptions struct {
	NetworkIDs   []string
	ProductTypes []string
	// Status filters by upgrade lifecycle: one or more of "started",
	// "completed", "canceled", "failed", "skipped", "scheduled".
	Status  []string
	Staged  *bool
	PerPage int
}

func (o FirmwareUpgradeOptions) values() url.Values {
	per := o.PerPage
	if per <= 0 {
		per = 1000
	}
	if per < 3 {
		per = 3
	}
	if per > 1000 {
		per = 1000
	}
	v := url.Values{"perPage": []string{strconv.Itoa(per)}}
	for _, n := range o.NetworkIDs {
		v.Add("networkIds[]", n)
	}
	for _, pt := range o.ProductTypes {
		v.Add("productTypes[]", pt)
	}
	for _, s := range o.Status {
		v.Add("status[]", s)
	}
	if o.Staged != nil {
		v.Set("staged", strconv.FormatBool(*o.Staged))
	}
	return v
}

// GetOrganizationFirmwareUpgrades paginates through the org's firmware
// upgrade events feed. Returns past + scheduled upgrades — the handler
// splits on `Status` to surface scheduled rows separately when needed.
func (c *Client) GetOrganizationFirmwareUpgrades(ctx context.Context, orgID string, opts FirmwareUpgradeOptions, ttl time.Duration) ([]FirmwareUpgrade, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/firmware/upgrades", Message: "missing organization id"}}
	}
	var out []FirmwareUpgrade
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/firmware/upgrades",
		orgID, opts.values(), ttl, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// FirmwareUpgradeByDevice is one row from
// `GET /organizations/{organizationId}/firmware/upgrades/byDevice`. Currently
// only MS + MR devices are returned (per Meraki's documented limitation as
// of 2026-04). The `Upgrade` field is the most recent / pending upgrade for
// the device — we surface scheduledFor + status so the panel can show a
// per-device pending-upgrade table.
type FirmwareUpgradeByDevice struct {
	Serial  string                       `json:"serial"`
	Name    string                       `json:"name,omitempty"`
	MAC     string                       `json:"mac,omitempty"`
	Model   string                       `json:"model,omitempty"`
	Network FirmwareUpgradeNetwork       `json:"network"`
	Upgrade FirmwareUpgradeByDeviceEntry `json:"upgrade"`
}

// FirmwareUpgradeByDeviceEntry is the per-device pending/most-recent upgrade
// envelope. `Time` is populated for completed upgrades; `ToVersion` carries
// the scheduledFor time on the version sub-object for scheduled rows.
type FirmwareUpgradeByDeviceEntry struct {
	UpgradeBatchID string                         `json:"upgradeBatchId,omitempty"`
	Time           *time.Time                     `json:"time,omitempty"`
	Status         string                         `json:"status,omitempty"`
	FromVersion    FirmwareVersionRef             `json:"fromVersion"`
	ToVersion      FirmwareUpgradeByDeviceVersion `json:"toVersion"`
	Staged         FirmwareUpgradeByDeviceStaged  `json:"staged"`
}

// FirmwareUpgradeByDeviceVersion adds a scheduledFor field to the standard
// version reference — Meraki puts the scheduled time inside the version
// envelope on the byDevice payload, not at the top level.
type FirmwareUpgradeByDeviceVersion struct {
	FirmwareVersionRef
	ScheduledFor *time.Time `json:"scheduledFor,omitempty"`
}

// FirmwareUpgradeByDeviceStaged is the staged-rollout group reference on each
// per-device upgrade entry. Empty `Group.ID` means the device is not part of
// a staged rollout.
type FirmwareUpgradeByDeviceStaged struct {
	Group FirmwareUpgradeStagedGroup `json:"group"`
}

// FirmwareUpgradeStagedGroup is `{id, name}` — the staged-upgrade group
// reference embedded on per-device upgrade entries.
type FirmwareUpgradeStagedGroup struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

// FirmwareUpgradesByDeviceOptions filters the per-device upgrade-status feed.
// `CurrentUpgradesOnly` short-circuits the response to in-progress + pending
// upgrades only — useful for the "what's about to happen" pending-upgrades
// table on the Firmware page.
type FirmwareUpgradesByDeviceOptions struct {
	NetworkIDs              []string
	Serials                 []string
	MACs                    []string
	FirmwareUpgradeBatchIDs []string
	UpgradeStatuses         []string
	CurrentUpgradesOnly     *bool
	LimitPerDevice          int
	PerPage                 int
}

func (o FirmwareUpgradesByDeviceOptions) values() url.Values {
	per := o.PerPage
	if per <= 0 {
		per = 1000
	}
	if per < 3 {
		per = 3
	}
	if per > 1000 {
		per = 1000
	}
	v := url.Values{"perPage": []string{strconv.Itoa(per)}}
	for _, n := range o.NetworkIDs {
		v.Add("networkIds[]", n)
	}
	for _, s := range o.Serials {
		v.Add("serials[]", s)
	}
	for _, m := range o.MACs {
		v.Add("macs[]", m)
	}
	for _, b := range o.FirmwareUpgradeBatchIDs {
		v.Add("firmwareUpgradeBatchIds[]", b)
	}
	for _, s := range o.UpgradeStatuses {
		v.Add("upgradeStatuses[]", s)
	}
	if o.CurrentUpgradesOnly != nil {
		v.Set("currentUpgradesOnly", strconv.FormatBool(*o.CurrentUpgradesOnly))
	}
	if o.LimitPerDevice > 0 {
		v.Set("limitPerDevice", strconv.Itoa(o.LimitPerDevice))
	}
	return v
}

// GetOrganizationFirmwareUpgradesByDevice paginates per-device upgrade
// status. Currently only MS + MR devices are present in the response
// (Meraki documented limitation 2026-04).
func (c *Client) GetOrganizationFirmwareUpgradesByDevice(ctx context.Context, orgID string, opts FirmwareUpgradesByDeviceOptions, ttl time.Duration) ([]FirmwareUpgradeByDevice, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/firmware/upgrades/byDevice", Message: "missing organization id"}}
	}
	var out []FirmwareUpgradeByDevice
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/firmware/upgrades/byDevice",
		orgID, opts.values(), ttl, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// InventoryDeviceEox is one row from
// `GET /organizations/{organizationId}/inventory/devices`. We surface a
// trimmed view focused on the EOX-relevant fields; the full inventory
// payload includes order numbers, claim metadata, and other commerce fields
// the Firmware page does not render.
//
// `EoxStatus` is one of "endOfSale", "endOfSupport", "nearEndOfSupport", or
// the empty string when Meraki has no EOX data on file. `EndOfSaleDate` and
// `EndOfSupportDate` are ISO-8601 date strings — decoded to `*time.Time`
// (the Go RFC3339 parser is permissive for date-only strings).
type InventoryDeviceEox struct {
	Serial           string     `json:"serial"`
	Name             string     `json:"name,omitempty"`
	MAC              string     `json:"mac,omitempty"`
	Model            string     `json:"model,omitempty"`
	NetworkID        string     `json:"networkId,omitempty"`
	ProductType      string     `json:"productType,omitempty"`
	EoxStatus        string     `json:"eoxStatus,omitempty"`
	EndOfSaleDate    *time.Time `json:"endOfSaleDate,omitempty"`
	EndOfSupportDate *time.Time `json:"endOfSupportDate,omitempty"`
}

// InventoryDevicesEoxOptions filters the inventory list to EOX-tagged
// devices. The inventory endpoint accepts the standard productTypes filter
// plus an `eoxStatuses` repeated query parameter; we expose both here. The
// handler defaults `EoxStatuses` to all three EOX buckets when callers leave
// it empty so the EOL table doesn't accidentally include healthy devices.
type InventoryDevicesEoxOptions struct {
	NetworkIDs   []string
	ProductTypes []string
	Serials      []string
	EoxStatuses  []string
	PerPage      int
}

func (o InventoryDevicesEoxOptions) values() url.Values {
	per := o.PerPage
	if per <= 0 {
		per = 1000
	}
	if per < 3 {
		per = 3
	}
	if per > 1000 {
		per = 1000
	}
	v := url.Values{"perPage": []string{strconv.Itoa(per)}}
	for _, n := range o.NetworkIDs {
		v.Add("networkIds[]", n)
	}
	for _, pt := range o.ProductTypes {
		v.Add("productTypes[]", pt)
	}
	for _, s := range o.Serials {
		v.Add("serials[]", s)
	}
	for _, s := range o.EoxStatuses {
		v.Add("eoxStatuses[]", s)
	}
	return v
}

// GetOrganizationInventoryDevicesEox lists inventory devices with EOX-status
// filters applied. Link-paginated. The handler typically invokes this with
// `EoxStatuses=["endOfSale","endOfSupport","nearEndOfSupport"]` so the
// returned slice already excludes healthy devices.
func (c *Client) GetOrganizationInventoryDevicesEox(ctx context.Context, orgID string, opts InventoryDevicesEoxOptions, ttl time.Duration) ([]InventoryDeviceEox, error) {
	if orgID == "" {
		return nil, &NotFoundError{APIError: APIError{Endpoint: "organizations/{organizationId}/inventory/devices", Message: "missing organization id"}}
	}
	var out []InventoryDeviceEox
	_, err := c.GetAll(ctx,
		"organizations/"+url.PathEscape(orgID)+"/inventory/devices",
		orgID, opts.values(), ttl, &out)
	if err != nil {
		return nil, err
	}
	return out, nil
}
