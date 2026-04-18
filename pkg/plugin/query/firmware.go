package query

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/data"

	"github.com/robknight/grafana-meraki-plugin/pkg/meraki"
)

// firmwareTTL — firmware state is slow-moving (Meraki publishes new versions
// on the order of weeks; EOL announcements on the order of months). 15m
// matches the licensing TTL — both surfaces refresh on roughly the same
// cadence and don't need second-resolution freshness.
const firmwareTTL = 15 * time.Minute

// daysUntil computes whole-day difference between t and now, truncating
// toward negative infinity. Returns 0 when t is the zero time. Used by both
// the EOL handler (daysUntil end-of-support) and the pending-upgrade
// handler (daysUntil scheduledFor).
func daysUntil(t time.Time, now time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	delta := t.Sub(now)
	// Truncate toward negative-infinity so a "1.5 days from now" reads as 1
	// rather than rounding up to 2.
	return int64(delta / (24 * time.Hour))
}

// handleFirmwareUpgrades emits a single table frame summarising past +
// scheduled firmware-upgrade events for the org. One row per upgrade event.
//
// Frame columns:
//
//	time, status, productType, networkId, networkName,
//	fromVersion, toVersion, releaseType, staged, upgradeId
//
// Decision (judgment call from the brief): we keep past + future in a
// single table and let the panel sort/filter by `status`. Splitting into
// two tables would force the operator to context-switch when reviewing a
// network-wide rollout — most of the value is in seeing "what was last done
// + what's coming next" side-by-side.
func handleFirmwareUpgrades(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("firmwareUpgrades: orgId is required")
	}

	opts := meraki.FirmwareUpgradeOptions{
		NetworkIDs:   firmwareFilterEmpty(q.NetworkIDs),
		ProductTypes: firmwareFilterEmpty(q.ProductTypes),
	}

	upgrades, err := client.GetOrganizationFirmwareUpgrades(ctx, q.OrgID, opts, firmwareTTL)
	if err != nil {
		return nil, err
	}

	var (
		times       []time.Time
		statuses    []string
		productType []string
		networkIDs  []string
		networkName []string
		fromVer     []string
		toVer       []string
		releaseType []string
		staged      []bool
		upgradeIDs  []string
	)

	for _, u := range upgrades {
		ts := time.Time{}
		if u.Time != nil && !u.Time.IsZero() {
			ts = u.Time.UTC()
		}
		times = append(times, ts)
		statuses = append(statuses, u.Status)
		productType = append(productType, u.ProductType)
		networkIDs = append(networkIDs, u.Network.ID)
		networkName = append(networkName, u.Network.Name)
		fromVer = append(fromVer, firstNonEmpty([]string{u.FromVersion.ShortName, u.FromVersion.Firmware, u.FromVersion.ID}))
		toVer = append(toVer, firstNonEmpty([]string{u.ToVersion.ShortName, u.ToVersion.Firmware, u.ToVersion.ID}))
		releaseType = append(releaseType, u.ToVersion.ReleaseType)
		staged = append(staged, u.Staged)
		upgradeIDs = append(upgradeIDs, u.UpgradeID)
	}

	return []*data.Frame{
		data.NewFrame("firmware_upgrades",
			data.NewField("time", nil, times),
			data.NewField("status", nil, statuses),
			data.NewField("productType", nil, productType),
			data.NewField("networkId", nil, networkIDs),
			data.NewField("networkName", nil, networkName),
			data.NewField("fromVersion", nil, fromVer),
			data.NewField("toVersion", nil, toVer),
			data.NewField("releaseType", nil, releaseType),
			data.NewField("staged", nil, staged),
			data.NewField("upgradeId", nil, upgradeIDs),
		),
	}, nil
}

// handleFirmwarePending emits a single table frame of devices with a
// pending or in-progress firmware upgrade. One row per device. Currently
// only MS + MR devices are returned by Meraki on this endpoint (their
// documented limitation as of 2026-04).
//
// Frame columns:
//
//	serial, name, model, productType, networkId, networkName,
//	currentVersion, targetVersion, scheduledFor, daysUntil,
//	status, stagedGroup
//
// `daysUntil` is computed at emit time (whole days from now to
// `scheduledFor`). The Firmware page binds threshold colour overrides to
// this column — <7d red, <30d amber — so the urgency of a pending rollout
// is visible without the operator having to mentally diff the timestamp
// against the wall clock.
func handleFirmwarePending(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("firmwarePending: orgId is required")
	}

	currentOnly := true
	opts := meraki.FirmwareUpgradesByDeviceOptions{
		NetworkIDs:          firmwareFilterEmpty(q.NetworkIDs),
		Serials:             firmwareFilterEmpty(q.Serials),
		CurrentUpgradesOnly: &currentOnly,
	}

	devs, err := client.GetOrganizationFirmwareUpgradesByDevice(ctx, q.OrgID, opts, firmwareTTL)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	var (
		serials     []string
		names       []string
		models      []string
		productType []string
		networkIDs  []string
		networkName []string
		current     []string
		target      []string
		scheduled   []time.Time
		daysFields  []int64
		statuses    []string
		stagedGroup []string
	)

	for _, d := range devs {
		// Skip devices where Meraki returned an empty `upgrade` envelope —
		// `currentUpgradesOnly=true` should already exclude these but the
		// field can still arrive zeroed-out for completed transitions that
		// just dropped off the queue.
		if d.Upgrade.Status == "" && d.Upgrade.ToVersion.ID == "" && d.Upgrade.UpgradeBatchID == "" {
			continue
		}

		serials = append(serials, d.Serial)
		names = append(names, d.Name)
		models = append(models, d.Model)
		// productType is not on the byDevice payload — we leave it empty so
		// the column stays present for forward-compat. The Model column
		// already disambiguates MS vs MR rows.
		productType = append(productType, "")
		networkIDs = append(networkIDs, d.Network.ID)
		networkName = append(networkName, d.Network.Name)
		current = append(current, firstNonEmpty([]string{d.Upgrade.FromVersion.ShortName, d.Upgrade.FromVersion.Firmware, d.Upgrade.FromVersion.ID}))
		target = append(target, firstNonEmpty([]string{d.Upgrade.ToVersion.ShortName, d.Upgrade.ToVersion.Firmware, d.Upgrade.ToVersion.ID}))

		var sf time.Time
		if d.Upgrade.ToVersion.ScheduledFor != nil && !d.Upgrade.ToVersion.ScheduledFor.IsZero() {
			sf = d.Upgrade.ToVersion.ScheduledFor.UTC()
		} else if d.Upgrade.Time != nil && !d.Upgrade.Time.IsZero() {
			sf = d.Upgrade.Time.UTC()
		}
		scheduled = append(scheduled, sf)
		daysFields = append(daysFields, daysUntil(sf, now))

		statuses = append(statuses, d.Upgrade.Status)
		stagedGroup = append(stagedGroup, d.Upgrade.Staged.Group.Name)
	}

	return []*data.Frame{
		data.NewFrame("firmware_pending",
			data.NewField("serial", nil, serials),
			data.NewField("name", nil, names),
			data.NewField("model", nil, models),
			data.NewField("productType", nil, productType),
			data.NewField("networkId", nil, networkIDs),
			data.NewField("networkName", nil, networkName),
			data.NewField("currentVersion", nil, current),
			data.NewField("targetVersion", nil, target),
			data.NewField("scheduledFor", nil, scheduled),
			data.NewField("daysUntil", nil, daysFields),
			data.NewField("status", nil, statuses),
			data.NewField("stagedGroup", nil, stagedGroup),
		),
	}, nil
}

// handleDeviceEol emits a single table frame of devices with EOX (end-of-X)
// status set. One row per device. Sorted by `daysUntil` ascending so the
// most urgent devices float to the top of the table.
//
// Frame columns:
//
//	serial, name, model, productType, networkId, eoxStatus,
//	endOfSaleDate, endOfSupportDate, daysUntil
//
// `daysUntil` is whole days from now to `endOfSupportDate`, falling back to
// `endOfSaleDate` when end-of-support is absent. Negative values mean the
// device is past end-of-support today.
//
// Defaults: when the caller leaves `q.Metrics` empty, we filter to the
// union of all three EOX buckets ("endOfSale", "endOfSupport",
// "nearEndOfSupport") so the table excludes healthy devices. Operators can
// pass a single-bucket filter via `q.Metrics[0]` to scope the table further.
func handleDeviceEol(ctx context.Context, client *meraki.Client, q MerakiQuery, _ TimeRange, _ Options) ([]*data.Frame, error) {
	if q.OrgID == "" {
		return nil, fmt.Errorf("deviceEol: orgId is required")
	}

	statuses := firmwareFilterEmpty(q.Metrics)
	if len(statuses) == 0 {
		statuses = []string{"endOfSale", "endOfSupport", "nearEndOfSupport"}
	}

	opts := meraki.InventoryDevicesEoxOptions{
		NetworkIDs:   firmwareFilterEmpty(q.NetworkIDs),
		ProductTypes: firmwareFilterEmpty(q.ProductTypes),
		Serials:      firmwareFilterEmpty(q.Serials),
		EoxStatuses:  statuses,
	}

	devs, err := client.GetOrganizationInventoryDevicesEox(ctx, q.OrgID, opts, firmwareTTL)
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()

	type eolRow struct {
		Serial      string
		Name        string
		Model       string
		ProductType string
		NetworkID   string
		EoxStatus   string
		EosDate     time.Time
		EouDate     time.Time
		Days        int64
	}

	rows := make([]eolRow, 0, len(devs))
	for _, d := range devs {
		var eos, eou time.Time
		if d.EndOfSaleDate != nil && !d.EndOfSaleDate.IsZero() {
			eos = d.EndOfSaleDate.UTC()
		}
		if d.EndOfSupportDate != nil && !d.EndOfSupportDate.IsZero() {
			eou = d.EndOfSupportDate.UTC()
		}
		// daysUntil prefers end-of-support (the operationally critical
		// deadline). Falls back to end-of-sale when support date is absent
		// — for the few devices Meraki has sale-date but no support-date
		// data on, this still surfaces the urgency correctly.
		ref := eou
		if ref.IsZero() {
			ref = eos
		}
		rows = append(rows, eolRow{
			Serial:      d.Serial,
			Name:        d.Name,
			Model:       d.Model,
			ProductType: d.ProductType,
			NetworkID:   d.NetworkID,
			EoxStatus:   d.EoxStatus,
			EosDate:     eos,
			EouDate:     eou,
			Days:        daysUntil(ref, now),
		})
	}

	// Sort ascending by daysUntil; rows with no date (Days==0 from a
	// zero-time ref) end up between negative (past) and positive (future)
	// which matches operator expectations — actionable rows on top.
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Days < rows[j].Days
	})

	var (
		serials     []string
		names       []string
		models      []string
		productType []string
		networkIDs  []string
		eoxStatuses []string
		eosDates    []time.Time
		eouDates    []time.Time
		daysFields  []int64
	)
	for _, r := range rows {
		serials = append(serials, r.Serial)
		names = append(names, r.Name)
		models = append(models, r.Model)
		productType = append(productType, r.ProductType)
		networkIDs = append(networkIDs, r.NetworkID)
		eoxStatuses = append(eoxStatuses, r.EoxStatus)
		eosDates = append(eosDates, r.EosDate)
		eouDates = append(eouDates, r.EouDate)
		daysFields = append(daysFields, r.Days)
	}

	return []*data.Frame{
		data.NewFrame("device_eol",
			data.NewField("serial", nil, serials),
			data.NewField("name", nil, names),
			data.NewField("model", nil, models),
			data.NewField("productType", nil, productType),
			data.NewField("networkId", nil, networkIDs),
			data.NewField("eoxStatus", nil, eoxStatuses),
			data.NewField("endOfSaleDate", nil, eosDates),
			data.NewField("endOfSupportDate", nil, eouDates),
			data.NewField("daysUntil", nil, daysFields),
		),
	}, nil
}

// firmwareFilterEmpty drops empty strings from a slice. Used to clean up
// scene variable expansions like `[""]` (the `allValue: ''` convention
// shared by every variable factory in src/scene-helpers/variables.ts).
//
// Named with a `firmware` prefix so it doesn't collide with the
// existing `firstNonEmpty` helper in alerts.go or any future generic
// helper that may land in dispatch.go.
func firmwareFilterEmpty(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
