import {
  PanelBuilders,
  SceneDataTransformer,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { FieldColorModeId, ThresholdsMode } from '@grafana/schema';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';

// Shared query-runner factories ---------------------------------------------

interface FirmwareQueryOpts {
  refId?: string;
  metrics?: string[];
}

/** SceneQueryRunner for the org-wide firmware/upgrades event feed. */
function firmwareUpgradesQuery(opts: FirmwareQueryOpts = {}): SceneQueryRunner {
  const { refId = 'A' } = opts;
  return new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [
      {
        refId,
        kind: QueryKind.FirmwareUpgrades,
        orgId: '$org',
        networkIds: ['$network'],
      },
    ],
  });
}

/** SceneQueryRunner for the per-device pending-upgrades feed. */
function firmwarePendingQuery(opts: FirmwareQueryOpts = {}): SceneQueryRunner {
  const { refId = 'A' } = opts;
  return new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [
      {
        refId,
        kind: QueryKind.FirmwarePending,
        orgId: '$org',
        networkIds: ['$network'],
      },
    ],
  });
}

/** SceneQueryRunner for the EOL device list. `metrics[0]` is the EOX-bucket
 *  filter (empty string ⇒ all three buckets). */
function deviceEolQuery(opts: FirmwareQueryOpts = {}): SceneQueryRunner {
  const { refId = 'A', metrics = ['$eoxStatus'] } = opts;
  return new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [
      {
        refId,
        kind: QueryKind.DeviceEol,
        orgId: '$org',
        networkIds: ['$network'],
        metrics,
      },
    ],
  });
}

// KPI tiles -----------------------------------------------------------------

/**
 * Generic stat tile — renders a single field from a runner that emits a
 * table frame, using the `reduce`-by-row count as the value.
 *
 * We intentionally avoid the "wide-frame organize → reduce" KPI pattern
 * here because firmware kinds emit row-per-event tables (one row per
 * upgrade event / pending device / EOL device), not single-row wide frames.
 * Counting the rows directly is the right reducer — `count` in
 * `reduceOptions.calcs` returns the row count of any non-null field.
 */
function rowCountStat(params: {
  title: string;
  runner: SceneQueryRunner;
  fieldName: string;
  thresholds?: Array<{ value: number; color: string }>;
}): VizPanel {
  const { title, runner, fieldName, thresholds = [] } = params;
  const builder = PanelBuilders.stat()
    .setTitle(title)
    .setData(runner)
    .setNoValue('0')
    .setOption('reduceOptions', {
      values: false,
      calcs: ['count'],
      fields: `/^${fieldName}$/`,
    } as any)
    .setOption('colorMode', 'value' as any);

  if (thresholds.length > 0) {
    builder.setColor({ mode: FieldColorModeId.Thresholds }).setThresholds({
      mode: ThresholdsMode.Absolute,
      steps: thresholds.map((t, i) => ({
        value: i === 0 ? (null as unknown as number) : t.value,
        color: t.color,
      })),
    });
  }

  return builder.build();
}

/**
 * KPI #1 — count of upgrades currently in the "scheduled" status.
 * Uses an `organize` filter on the firmware/upgrades feed so the row count
 * reflects scheduled rows only.
 */
export function firmwareScheduledCountStat(): VizPanel {
  const runner = firmwareUpgradesQuery({ refId: 'kpi_scheduled' });
  const filtered = new SceneDataTransformer({
    $data: runner,
    transformations: [
      {
        id: 'filterByValue',
        options: {
          filters: [
            {
              fieldName: 'status',
              config: {
                id: 'equal',
                options: { value: 'scheduled' },
              },
            },
          ],
          type: 'include',
          match: 'all',
        },
      },
    ],
  });
  return PanelBuilders.stat()
    .setTitle('Upgrades scheduled')
    .setDescription('Count of org-wide firmware upgrades currently in the "scheduled" status.')
    .setData(filtered)
    .setNoValue('0')
    .setOption('reduceOptions', {
      values: false,
      calcs: ['count'],
      fields: '/^status$/',
    } as any)
    .setOption('colorMode', 'value' as any)
    .setColor({ mode: FieldColorModeId.Thresholds })
    .setThresholds({
      mode: ThresholdsMode.Absolute,
      steps: [
        { value: null as unknown as number, color: 'green' },
        { value: 1, color: 'blue' },
      ],
    })
    .build();
}

/** KPI #2 — count of devices with a current pending firmware upgrade. */
export function firmwarePendingCountStat(): VizPanel {
  const runner = firmwarePendingQuery({ refId: 'kpi_pending' });
  return rowCountStat({
    title: 'Devices pending upgrade',
    runner,
    fieldName: 'serial',
    thresholds: [
      { value: 0, color: 'green' },
      { value: 1, color: 'orange' },
    ],
  });
}

/**
 * KPI #3 — count of devices end-of-support within the next 90 days.
 *
 * The EOL feed already returns a `daysUntil` column server-side; we wrap
 * the runner in a `filterByValue` transform to keep rows where
 * `daysUntil <= 90`, then count remaining rows. `filterByValue` is safe
 * here (vs the §G.20 warning on `filterByValue + reduce`) because we are
 * counting rows, not reducing a numeric field — there is no reducer
 * ambiguity to misfire on.
 */
export function firmwareEolSoonCountStat(): VizPanel {
  const runner = deviceEolQuery({ refId: 'kpi_eol' });
  const filtered = new SceneDataTransformer({
    $data: runner,
    transformations: [
      {
        id: 'filterByValue',
        options: {
          filters: [
            {
              fieldName: 'daysUntil',
              config: {
                id: 'lowerOrEqual',
                options: { value: 90 },
              },
            },
          ],
          type: 'include',
          match: 'all',
        },
      },
    ],
  });
  return PanelBuilders.stat()
    .setTitle('EOL ≤ 90 days')
    .setDescription(
      'Count of devices reaching end-of-support within 90 days. Includes ' +
        'devices already past end-of-support (negative daysUntil).'
    )
    .setData(filtered)
    .setNoValue('0')
    .setOption('reduceOptions', {
      values: false,
      calcs: ['count'],
      fields: '/^serial$/',
    } as any)
    .setOption('colorMode', 'value' as any)
    .setColor({ mode: FieldColorModeId.Thresholds })
    .setThresholds({
      mode: ThresholdsMode.Absolute,
      steps: [
        { value: null as unknown as number, color: 'green' },
        { value: 1, color: 'orange' },
        { value: 5, color: 'red' },
      ],
    })
    .build();
}

// Tables --------------------------------------------------------------------

/**
 * Pending upgrades table — one row per device with a current/in-progress
 * upgrade. Threshold-coloured `daysUntil` column makes urgency visible:
 *   - red:    daysUntil < 7  (rollout starts within a week)
 *   - amber:  daysUntil < 30
 *   - green:  daysUntil >= 30
 *
 * Threshold values are a judgment call (per the brief) — chosen to match
 * the cadence of weekly maintenance windows + monthly change-management
 * meetings most operators run on.
 */
export function firmwarePendingTable(): VizPanel {
  const runner = firmwarePendingQuery();
  return PanelBuilders.table()
    .setTitle('Pending upgrades')
    .setDescription(
      'Devices with a pending or in-progress firmware upgrade. Currently ' +
        'limited to MS + MR devices per Meraki API restrictions (2026-04). ' +
        '`daysUntil` is coloured to flag rollouts under one week as critical ' +
        'and rollouts under one month as approaching.'
    )
    .setData(runner)
    .setNoValue('No pending firmware upgrades.')
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('name').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('model').overrideCustomFieldConfig('width', 140);
      b.matchFieldsWithName('currentVersion').overrideCustomFieldConfig('width', 140);
      b.matchFieldsWithName('targetVersion').overrideCustomFieldConfig('width', 140);
      b.matchFieldsWithName('scheduledFor').overrideCustomFieldConfig('width', 200);
      b.matchFieldsWithName('status').overrideCustomFieldConfig('width', 120);
      b.matchFieldsWithName('stagedGroup').overrideCustomFieldConfig('width', 140);

      b.matchFieldsWithName('daysUntil')
        .overrideCustomFieldConfig('width', 110)
        .overrideColor({ mode: FieldColorModeId.Thresholds })
        .overrideThresholds({
          mode: ThresholdsMode.Absolute,
          steps: [
            { value: null as unknown as number, color: 'red' },
            { value: 7, color: 'orange' },
            { value: 30, color: 'green' },
          ],
        })
        .overrideCustomFieldConfig('cellOptions', { type: 'color-background' } as any);
    })
    .setOption('sortBy', [{ displayName: 'daysUntil', desc: false }] as any)
    .build();
}

/**
 * EOL devices table — one row per device with an EOX status set, sorted
 * server-side by `daysUntil` ascending so devices already past
 * end-of-support float to the top. The `daysUntil` column is
 * threshold-coloured the same way as the pending-upgrade table for visual
 * consistency, but with a slightly more relaxed amber band — EOX timelines
 * are measured in months, not weeks.
 */
export function deviceEolTable(): VizPanel {
  const runner = deviceEolQuery();
  return PanelBuilders.table()
    .setTitle('End-of-life devices')
    .setDescription(
      'Devices flagged by Meraki as end-of-sale, end-of-support, or ' +
        'near-end-of-support. Sorted by daysUntil end-of-support ' +
        'ascending; negative values mean the device is already past ' +
        'end-of-support today. Source: ' +
        '/organizations/{id}/inventory/devices?eoxStatuses[]=…'
    )
    .setData(runner)
    .setNoValue('No EOL devices in the selected scope.')
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('name').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('model').overrideCustomFieldConfig('width', 140);
      b.matchFieldsWithName('productType').overrideCustomFieldConfig('width', 130);
      b.matchFieldsWithName('eoxStatus').overrideCustomFieldConfig('width', 160);
      b.matchFieldsWithName('endOfSaleDate').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('endOfSupportDate').overrideCustomFieldConfig('width', 180);

      b.matchFieldsWithName('daysUntil')
        .overrideCustomFieldConfig('width', 110)
        .overrideColor({ mode: FieldColorModeId.Thresholds })
        .overrideThresholds({
          mode: ThresholdsMode.Absolute,
          steps: [
            { value: null as unknown as number, color: 'red' },
            { value: 30, color: 'orange' },
            { value: 180, color: 'green' },
          ],
        })
        .overrideCustomFieldConfig('cellOptions', { type: 'color-background' } as any);
    })
    .setOption('sortBy', [{ displayName: 'daysUntil', desc: false }] as any)
    .build();
}
