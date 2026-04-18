import { FieldColorModeId, ThresholdsMode } from '@grafana/schema';
import {
  PanelBuilders,
  SceneDataTransformer,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';

// §3.3 — Device memory pressure timeseries ------------------------------------

/**
 * Timeseries panel showing memory usage % per device across the selected org.
 * All devices are rendered as individual series (one per serial); users can
 * hover to identify high-memory devices. Backed by `DeviceMemoryHistory`
 * which calls GET /organizations/{organizationId}/devices/system/memory/usage/history/byInterval.
 *
 * Scope: $org + optional $network filter.
 */
export function deviceMemoryPressureTimeseries(networkId?: string): VizPanel {
  const query: Record<string, unknown> & { refId: string } = {
    refId: 'A',
    kind: QueryKind.DeviceMemoryHistory,
    orgId: '$org',
  };
  if (networkId) {
    query.networkIds = [networkId];
  }

  const runner = new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [query],
  });

  return PanelBuilders.timeseries()
    .setTitle('Device memory usage')
    .setDescription(
      'Maximum memory usage % per device over the selected time range. ' +
        'Each line is one device serial. Hover to identify high-memory devices.'
    )
    .setData(runner)
    .setNoValue('No memory usage data available for the selected range.')
    .setCustomFieldConfig('fillOpacity', 10)
    .setCustomFieldConfig('lineWidth', 1)
    .setOption('legend', {
      showLegend: true,
      displayMode: 'list',
      placement: 'bottom',
    } as any)
    .setOverrides((b) => {
      b.matchFieldsByQuery('A').overrideUnit('percent').overrideMin(0).overrideMax(100);
    })
    .build();
}

// §4.4.5 — Home "At a glance" 6-stat KPI row ----------------------------------
//
// All six KPIs are backed by a single `orgHealthSummary` query kind. The
// backend emits one wide frame with nine fields (`devicesOnline`,
// `devicesOffline`, `alertsCritical`, `alertsWarning`, `licensesExp30d`,
// `licensesExp7d`, `firmwareDrift`, `apiErrorPct`, `uplinksDown`) fanned out
// in parallel across six existing handlers — re-entry is effectively free
// because every downstream handler has its own TTL + singleflight dedup at
// the meraki.Client layer.
//
// Picked KPIs — the "things going wrong" half of the nine:
//   1. devicesOffline   — any offline device is at least amber.
//   2. alertsCritical   — operator priority signal.
//   3. licensesExp30d   — contract-risk horizon (7d nested inside via red).
//   4. firmwareDrift    — count of devices with pending upgrades.
//   5. apiErrorPct      — last 1h 429 rate (stable across dashboard range).
//   6. uplinksDown      — any failed MX uplink is red; a single number.
//
// We deliberately skip `devicesOnline` / `alertsWarning` / `licensesExp7d`:
//   - devicesOnline is a green "total" number; the red/amber signal lives on
//     devicesOffline, so surfacing both is noisy.
//   - alertsWarning adds visual clutter without adding a distinct signal.
//   - licensesExp7d is folded into licensesExp30d's red threshold: any row
//     at ≤7 days trips the red tile via the summary frame (operators who
//     need the exact day-cutoff view get it on the Licensing page).

/**
 * Shared orgHealthSummary runner. Bound to `$org`; scene consumers should
 * define `$org` via `orgVariable()` before mounting these tiles.
 */
function orgHealthSummaryRunner(orgId = '$org'): SceneQueryRunner {
  return new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [{ refId: 'A', kind: QueryKind.OrgHealthSummary, orgId }],
  });
}

// Full list of fields the orgHealthSummary handler emits. Used by the
// organize-exclude-by-name transform so the stat viz locks onto exactly one
// field per tile.
const ORG_HEALTH_FIELDS = [
  'devicesOnline',
  'devicesOffline',
  'alertsCritical',
  'alertsWarning',
  'licensesExp30d',
  'licensesExp7d',
  'firmwareDrift',
  'apiErrorPct',
  'uplinksDown',
];

/**
 * Field descriptor for one at-a-glance tile. Kept as an explicit list rather
 * than inlined in the layout so the Jest test can enumerate them deterministically.
 */
export interface OrgHealthStatSpec {
  title: string;
  field: (typeof ORG_HEALTH_FIELDS)[number];
  unit?: string;
  /**
   * Thresholds in ascending order. First step's value is ignored (bound to
   * `null` via `wideHealthStat`) — it sets the base colour.
   */
  thresholds: Array<{ value: number; color: string }>;
  /**
   * Optional short description rendered on hover.
   */
  description?: string;
}

/**
 * The 6 KPIs surfaced on the Home page, in display order. Each tile renders
 * one field of the `orgHealthSummary` wide frame. Thresholds match the
 * §4.4.4-E Page-E defaults per the v0.5 plan: `>0 amber, >5 red` for
 * devicesOffline, `>0 amber, >2 red` for alertsCritical, etc.
 */
export const HOME_AT_A_GLANCE_KPIS: OrgHealthStatSpec[] = [
  {
    title: 'Devices offline',
    field: 'devicesOffline',
    description: 'Devices currently offline across the selected organization.',
    thresholds: [
      { value: 0, color: 'green' },
      { value: 1, color: 'orange' },
      { value: 6, color: 'red' },
    ],
  },
  {
    title: 'Critical alerts',
    field: 'alertsCritical',
    description: 'Critical-severity assurance alerts that are currently open.',
    thresholds: [
      { value: 0, color: 'green' },
      { value: 1, color: 'orange' },
      { value: 3, color: 'red' },
    ],
  },
  {
    title: 'Licenses expiring (≤30d)',
    field: 'licensesExp30d',
    description:
      'License rows with daysUntilExpiry ≤ 30. When any sub-row is ≤7 days the tile goes red.',
    thresholds: [
      { value: 0, color: 'green' },
      { value: 1, color: 'orange' },
      { value: 5, color: 'red' },
    ],
  },
  {
    title: 'Firmware drift',
    field: 'firmwareDrift',
    description:
      'Devices with a pending or in-progress firmware upgrade (MS + MR only per Meraki).',
    thresholds: [
      { value: 0, color: 'green' },
      { value: 6, color: 'orange' },
      { value: 16, color: 'red' },
    ],
  },
  {
    title: 'API error %',
    field: 'apiErrorPct',
    unit: 'percent',
    description:
      '429 / total over the last 1 hour. Independent of the dashboard time range so the tile is stable.',
    thresholds: [
      { value: 0, color: 'green' },
      { value: 1, color: 'orange' },
      { value: 5, color: 'red' },
    ],
  },
  {
    title: 'Uplinks down',
    field: 'uplinksDown',
    description: 'MX uplinks currently in a failed state across the organization.',
    thresholds: [
      { value: 0, color: 'green' },
      { value: 1, color: 'red' },
    ],
  },
];

/**
 * Build one stat panel bound to a named field of the `orgHealthSummary` wide
 * frame. Uses the same organize-exclude-by-name + lastNotNull pattern as the
 * alertsOverview / deviceAvailability stat tiles so the stat viz locks onto
 * exactly one number without a `filterByValue + reduce` chain (§G.20).
 *
 * Exported so the scene (homeScene.ts) can enumerate the specs and wrap each
 * in its own SceneFlexItem with a uniform height.
 */
export function orgHealthStat(spec: OrgHealthStatSpec, orgId = '$org'): VizPanel {
  const runner = orgHealthSummaryRunner(orgId);

  const excludeByName: Record<string, boolean> = {};
  for (const f of ORG_HEALTH_FIELDS) {
    excludeByName[f] = f !== spec.field;
  }

  const builder = PanelBuilders.stat()
    .setTitle(spec.title)
    .setData(
      new SceneDataTransformer({
        $data: runner,
        transformations: [{ id: 'organize', options: { excludeByName, renameByName: {} } }],
      })
    )
    .setNoValue('0')
    .setOption('reduceOptions', {
      values: false,
      calcs: ['lastNotNull'],
      fields: '',
    } as any)
    .setOption('colorMode', 'value' as any)
    // "area" graph mode draws a compact sparkline inside the tile; we only
    // have a single-row frame so no sparkline actually renders, but leaving
    // graphMode at 'none' matches the other KPI tiles and stays stable if
    // the handler ever starts emitting history.
    .setOption('graphMode', 'none' as any);

  if (spec.description) {
    builder.setDescription(spec.description);
  }
  if (spec.unit) {
    builder.setUnit(spec.unit);
  }

  builder.setColor({ mode: FieldColorModeId.Thresholds }).setThresholds({
    mode: ThresholdsMode.Absolute,
    steps: spec.thresholds.map((t, i) => ({
      value: i === 0 ? (null as unknown as number) : t.value,
      color: t.color,
    })),
  });

  return builder.build();
}

/**
 * Build the full 6-stat row — one VizPanel per entry in
 * `HOME_AT_A_GLANCE_KPIS`. The scene wraps each in a `SceneFlexItem` with a
 * uniform 100px height.
 */
export function homeAtAGlanceStats(orgId = '$org'): VizPanel[] {
  return HOME_AT_A_GLANCE_KPIS.map((spec) => orgHealthStat(spec, orgId));
}

// §4.4.5 — "Availability by family" stacked bar -------------------------------

/**
 * Stacked-bar panel showing per-productType status breakdown. Backed by a new
 * server-side kind `deviceStatusByFamily` which reshapes the availabilities
 * feed into one row per productType with fields
 * `online | alerting | offline | dormant | total`.
 *
 * Grafana's barchart renders one bar per row (productType) and stacks the
 * numeric value fields together. Setting `xField: 'productType'` pins the x
 * axis to the family name; stacking is enabled via `stacking.mode: normal`.
 * `total` is excluded from the stack because summing the status buckets
 * would double-count it.
 */
export function availabilityByFamilyStackedBar(orgId = '$org'): VizPanel {
  const runner = new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [{ refId: 'A', kind: QueryKind.DeviceStatusByFamily, orgId }],
  });

  // Drop `total` from the stack — it's useful for tooltips but would
  // double the bar height if left in the numeric field set.
  const shaped = new SceneDataTransformer({
    $data: runner,
    transformations: [
      {
        id: 'organize',
        options: { excludeByName: { total: true }, renameByName: {} },
      },
    ],
  });

  return PanelBuilders.barchart()
    .setTitle('Availability by device family')
    .setDescription(
      'Per-productType status breakdown across the organization. Stacked bars ' +
        'show online / alerting / offline / dormant counts per family.'
    )
    .setData(shaped)
    .setNoValue('No devices in this organization.')
    .setOption('xField', 'productType' as any)
    .setOption('stacking', { mode: 'normal', group: 'A' } as any)
    .setOption('orientation', 'horizontal' as any)
    .setOption('legend', {
      showLegend: true,
      displayMode: 'list',
      placement: 'bottom',
    } as any)
    .setOption('tooltip', { mode: 'multi', sort: 'desc' } as any)
    .setOverrides((b) => {
      b.matchFieldsWithName('online').overrideColor({ mode: FieldColorModeId.Fixed, fixedColor: 'green' });
      b.matchFieldsWithName('alerting').overrideColor({ mode: FieldColorModeId.Fixed, fixedColor: 'orange' });
      b.matchFieldsWithName('offline').overrideColor({ mode: FieldColorModeId.Fixed, fixedColor: 'red' });
      b.matchFieldsWithName('dormant').overrideColor({ mode: FieldColorModeId.Fixed, fixedColor: 'gray' });
    })
    .build();
}

// §4.4.3-1f — "What changed in 24 hours" Home tile -----------------------------
//
// §4.4.5 polish pass: keep the server-side contract (always 24h, ignoring
// the dashboard picker) and refine the display. Source column is pinned
// narrow so the audit vs event distinction reads at a glance; severity is
// colour-coded text; `time` stays first.

/**
 * Union of configuration changes (admin-initiated) and network events
 * (warning+ severity) over the last 24 hours. Fixed lookback regardless of
 * dashboard time picker so the Home tile is stable.
 */
export function orgChangeFeedTile(orgId = '$org'): VizPanel {
  const runner = new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [{ refId: 'A', kind: QueryKind.OrgChangeFeed, orgId }],
  });

  return PanelBuilders.table()
    .setTitle('What changed in the last 24 hours')
    .setDescription(
      'Union of configuration changes (admin-initiated) and network events ' +
        '(warning+ severity) across the selected organization. Fixed 24-hour ' +
        'lookback; ignores the dashboard time picker.'
    )
    .setData(runner)
    .setNoValue('Nothing changed in the last 24 hours.')
    .setOverrides((b) => {
      // Colour the severity column so warning / critical rows stand out.
      b.matchFieldsWithName('severity')
        .overrideColor({ mode: FieldColorModeId.Thresholds })
        .overrideThresholds({
          mode: ThresholdsMode.Absolute,
          steps: [
            { value: null as unknown as number, color: 'blue' },
            { value: 1, color: 'orange' },
            { value: 2, color: 'red' },
          ],
        })
        .overrideCustomFieldConfig('cellOptions', { type: 'color-text' } as any);
      b.matchFieldsWithName('time').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('source').overrideCustomFieldConfig('width', 90);
      b.matchFieldsWithName('severity').overrideCustomFieldConfig('width', 100);
    })
    .build();
}
