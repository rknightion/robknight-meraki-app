import { FieldColorModeId, ThresholdsMode } from '@grafana/schema';
import {
  PanelBuilders,
  SceneDataTransformer,
  SceneFlexItem,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';
import type { MerakiProductType, SensorMetric } from '../../types';
import { HideWhenEmpty } from '../Sensors/behaviors';

// Shared query-runner factory --------------------------------------------------
//
// This mirrors `oneQuery(...)` in `src/scene-helpers/panels.ts`. We keep a
// local copy so the Access Points area doesn't depend on an internal helper
// in the shared panels module (which other agents are mutating in parallel
// during Wave 3).

interface QueryParams {
  refId?: string;
  kind: QueryKind;
  orgId?: string;
  networkIds?: string[];
  serials?: string[];
  productTypes?: MerakiProductType[];
  metrics?: SensorMetric[];
  timespanSeconds?: number;
  maxDataPoints?: number;
}

function oneQuery(params: QueryParams): SceneQueryRunner {
  const {
    refId = 'A',
    kind,
    orgId,
    networkIds,
    serials,
    productTypes,
    metrics,
    timespanSeconds,
    maxDataPoints,
  } = params;

  const query: Record<string, unknown> & { refId: string } = { refId, kind };
  if (kind !== QueryKind.Organizations) {
    query.orgId = orgId ?? '$org';
  }
  if (networkIds && networkIds.length > 0) {
    query.networkIds = networkIds;
  }
  if (serials && serials.length > 0) {
    query.serials = serials;
  }
  if (productTypes && productTypes.length > 0) {
    query.productTypes = productTypes;
  }
  if (metrics && metrics.length > 0) {
    query.metrics = metrics;
  }
  if (typeof timespanSeconds === 'number') {
    query.timespanSeconds = timespanSeconds;
  }

  return new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [query],
    ...(maxDataPoints ? { maxDataPoints } : {}),
  });
}

function hideColumns(runner: SceneQueryRunner, columns: string[]): SceneDataTransformer {
  const excludeByName: Record<string, boolean> = {};
  for (const c of columns) {
    excludeByName[c] = true;
  }
  return new SceneDataTransformer({
    $data: runner,
    transformations: [{ id: 'organize', options: { excludeByName, renameByName: {} } }],
  });
}

// AP KPI row -----------------------------------------------------------------

/**
 * One KPI stat built from the `DeviceAvailabilities` frame (one row per
 * device) by filtering on the `status` column client-side.
 *
 * We intentionally lean on `filterByValue` + `reduce` here (rather than a
 * dedicated server-side aggregate kind like `SensorAlertSummary`) because the
 * availability frame is small (<= few thousand rows on the largest estates)
 * and the filterByValue reducer is reliable for the `count rows` case that
 * the sensor bug-report (todos.txt §G.20) specifically called out. If this
 * turns out to be flaky on a particular Grafana version we can promote it to
 * a `WirelessAvailabilitySummary` handler without touching the frontend
 * contract.
 */
function availabilityStat(
  title: string,
  status: 'online' | 'alerting' | 'offline' | 'dormant',
  thresholds: Array<{ value: number; color: string }>
): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.DeviceAvailabilities,
    productTypes: ['wireless'],
  });

  const builder = PanelBuilders.stat()
    .setTitle(title)
    .setData(
      new SceneDataTransformer({
        $data: runner,
        transformations: [
          {
            id: 'filterByValue',
            options: {
              filters: [
                {
                  fieldName: 'status',
                  config: { id: 'equal', options: { value: status } },
                },
              ],
              type: 'include',
              match: 'all',
            },
          },
          {
            id: 'reduce',
            options: {
              reducers: ['count'],
              fields: 'serial',
              mode: 'reduceFields',
              includeTimeField: false,
            },
          },
        ],
      })
    )
    .setNoValue('0')
    .setOption('reduceOptions', {
      values: false,
      calcs: ['lastNotNull'],
      fields: '',
    } as any)
    .setOption('colorMode', 'value' as any);

  if (thresholds.length > 0) {
    builder
      .setColor({ mode: FieldColorModeId.Thresholds })
      .setThresholds({
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
 * KPI row for the Access Points overview: three stat panels with counts of
 * wireless devices in each Meraki-reported status bucket. Consumers wrap
 * each panel in a `SceneCSSGridItem` to lay out a dense row.
 */
export function apStatusKpiRow(): VizPanel[] {
  return [
    availabilityStat('APs online', 'online', [
      { value: 0, color: 'red' },
      { value: 1, color: 'green' },
    ]),
    availabilityStat('APs alerting', 'alerting', [
      { value: 0, color: 'green' },
      { value: 1, color: 'orange' },
    ]),
    availabilityStat('APs offline', 'offline', [
      { value: 0, color: 'green' },
      { value: 1, color: 'red' },
    ]),
  ];
}

// AP inventory table ---------------------------------------------------------

/**
 * Table of every MR device in the selected org. Serial column drills into
 * the per-AP detail scene; mac/lat/lng columns are hidden because they're
 * rarely useful in a dense inventory view.
 */
export function apInventoryTable(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.Devices,
    productTypes: ['wireless'],
  });
  return PanelBuilders.table()
    .setTitle('Access point inventory')
    .setDescription('MR wireless devices in the selected organization. Click a serial to drill in.')
    .setData(hideColumns(runner, ['lat', 'lng', 'mac']))
    .setNoValue('No wireless devices found in this organization.')
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideLinks([
        {
          title: 'Open access point',
          url: `${PLUGIN_BASE_URL}/${ROUTES.AccessPoints}/\${__value.raw:percentencode}?var-org=\${var-org:queryparam}`,
        },
      ]);
      b.matchFieldsWithName('firmware').overrideCustomFieldConfig('width', 140);
      b.matchFieldsWithName('address').overrideCustomFieldConfig('width', 260);
    })
    .build();
}

// Channel utilisation + SSID usage -------------------------------------------

/**
 * Channel utilisation timeseries — one frame per (serial, band) from the
 * Wireless handler, legended via DisplayNameFromDS which the handler bakes
 * to `"<name> / <band> GHz"`. `$ap` is optional; when the All sentinel is
 * picked, serials is empty and the handler returns the full org snapshot.
 */
export function networkChannelUtilTimeseries(): VizPanel {
  return PanelBuilders.timeseries()
    .setTitle('Channel utilisation')
    .setDescription(
      'Per-AP per-band 802.11 channel utilisation. Drag-select a range to zoom; click a series to isolate it.'
    )
    .setData(
      oneQuery({
        kind: QueryKind.WirelessChannelUtil,
        serials: ['$ap'],
        maxDataPoints: 500,
      })
    )
    .setUnit('percent')
    .setMin(0)
    .setMax(100)
    .setNoValue('No channel utilisation data in the selected range.')
    .setColor({ mode: FieldColorModeId.PaletteClassic })
    .setCustomFieldConfig('lineWidth', 1)
    .setCustomFieldConfig('fillOpacity', 10)
    .setCustomFieldConfig('spanNulls', true)
    .setCustomFieldConfig('showPoints', 'auto' as any)
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .setOption('tooltip', { mode: 'multi', sort: 'desc' } as any)
    .build();
}

/**
 * Stacked per-network wireless usage timeseries. Each frame is one network's
 * total kbps; stacking makes it easy to see the aggregate and the per-network
 * share at a glance.
 */
export function ssidUsageStackedTimeseries(): VizPanel {
  return PanelBuilders.timeseries()
    .setTitle('Wireless usage')
    .setDescription('Aggregate wireless throughput per selected network (downstream + upstream).')
    .setData(
      oneQuery({
        kind: QueryKind.WirelessUsage,
        networkIds: ['$network'],
        maxDataPoints: 500,
      })
    )
    .setUnit('Kbits')
    .setNoValue('No usage data in the selected range.')
    .setColor({ mode: FieldColorModeId.PaletteClassic })
    .setCustomFieldConfig('lineWidth', 1)
    .setCustomFieldConfig('fillOpacity', 30)
    .setCustomFieldConfig('spanNulls', true)
    .setCustomFieldConfig('stacking', { mode: 'normal', group: 'A' } as any)
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .setOption('tooltip', { mode: 'multi', sort: 'desc' } as any)
    .build();
}

// Per-AP detail panels -------------------------------------------------------

/**
 * Table of clients currently associated with one AP. `timespanSeconds` is
 * left unset so the backend falls back to its default window.
 */
export function apClientsTable(serial: string): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.ApClients,
    serials: [serial],
  });
  return PanelBuilders.table()
    .setTitle('Clients')
    .setDescription('Stations currently associated with this access point.')
    .setData(hideColumns(runner, ['vlan']))
    .setNoValue('No clients associated with this AP in the selected range.')
    .setOverrides((b) => {
      b.matchFieldsWithName('mac').overrideCustomFieldConfig('width', 160);
      b.matchFieldsWithName('ip').overrideCustomFieldConfig('width', 140);
    })
    .build();
}

/**
 * Channel utilisation timeseries scoped to one AP, optionally filtered to a
 * single band. When no band filter is passed (the default), the panel shows
 * every band the AP reports on.
 */
function apChannelUtilPanel(serial: string, band?: string): VizPanel {
  const title = band ? `Channel utilisation — ${band} GHz` : 'Channel utilisation';
  return PanelBuilders.timeseries()
    .setTitle(title)
    .setData(
      oneQuery({
        kind: QueryKind.WirelessChannelUtil,
        serials: [serial],
        maxDataPoints: 500,
      })
    )
    .setUnit('percent')
    .setMin(0)
    .setMax(100)
    .setNoValue('No channel utilisation reported for this band.')
    .setColor({ mode: FieldColorModeId.PaletteClassic })
    .setCustomFieldConfig('lineWidth', 2)
    .setCustomFieldConfig('fillOpacity', 15)
    .setCustomFieldConfig('spanNulls', true)
    .setCustomFieldConfig('showPoints', 'auto' as any)
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .setOption('tooltip', { mode: 'multi', sort: 'desc' } as any)
    .build();
}

/**
 * One panel per wireless band for a single AP, each wrapped in a flex item
 * with the `HideWhenEmpty` behavior so silent bands collapse to zero height
 * rather than leaving empty chart real estate. Defaults to the three Wi-Fi
 * bands (2.4/5/6 GHz) which matches the `$band` variable's vocabulary.
 *
 * Grafana does server-side variable interpolation on the query kind's
 * label-filter, but the handler's labelling is what drives the legend — the
 * per-band title on each panel here is purely presentational.
 */
export function apRfPanels(serial: string, bands: string[] = ['2.4', '5', '6']): SceneFlexItem[] {
  return bands.map(
    (band) =>
      new SceneFlexItem({
        minHeight: 220,
        body: apChannelUtilPanel(serial, band),
        $behaviors: [new HideWhenEmpty()],
      })
  );
}

// Per-AP overview KPIs -------------------------------------------------------

/**
 * KPI tiles for one AP — derived from the `Devices` table filtered to one
 * serial. Each stat reads a specific column out of the single-row frame via
 * a reduce/filterFieldsByName chain so the stat viz picks up exactly one
 * value.
 *
 * We include model + networkId as labels-oriented stats rather than a header
 * card so the same panel machinery handles every tile; Scenes' stat viz
 * happily renders a string value as the displayed text.
 */
export function apOverviewKpiRow(serial: string): VizPanel[] {
  const runner = oneQuery({
    kind: QueryKind.Devices,
    serials: [serial],
    productTypes: ['wireless'],
  });

  function pick(title: string, field: string): VizPanel {
    return PanelBuilders.stat()
      .setTitle(title)
      .setData(
        new SceneDataTransformer({
          $data: runner,
          transformations: [
            {
              id: 'filterFieldsByName',
              options: {
                include: {
                  // Include just the field we want for this tile.
                  names: [field],
                },
              },
            },
          ],
        })
      )
      .setNoValue('—')
      .setOption('reduceOptions', {
        values: false,
        calcs: ['lastNotNull'],
        fields: '',
      } as any)
      .setOption('colorMode', 'none' as any)
      .build();
  }

  const availabilityRunner = oneQuery({
    kind: QueryKind.DeviceAvailabilities,
    serials: [serial],
    productTypes: ['wireless'],
  });

  const status = PanelBuilders.stat()
    .setTitle('Status')
    .setData(
      new SceneDataTransformer({
        $data: availabilityRunner,
        transformations: [
          {
            id: 'filterFieldsByName',
            options: { include: { names: ['status'] } },
          },
        ],
      })
    )
    .setNoValue('unknown')
    .setOption('reduceOptions', {
      values: false,
      calcs: ['lastNotNull'],
      fields: '',
    } as any)
    .setOption('colorMode', 'background' as any)
    .setMappings([
      {
        type: 'value' as any,
        options: {
          online: { color: 'green', index: 0, text: 'Online' },
          alerting: { color: 'orange', index: 1, text: 'Alerting' },
          offline: { color: 'red', index: 2, text: 'Offline' },
          dormant: { color: 'blue', index: 3, text: 'Dormant' },
        },
      },
    ])
    .build();

  return [status, pick('Model', 'model'), pick('Network', 'networkId'), pick('Firmware', 'firmware')];
}
