import { FieldColorModeId } from '@grafana/schema';
import {
  PanelBuilders,
  SceneDataTransformer,
  SceneFlexItem,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { deviceAvailabilityStat } from '../../scene-helpers/panels';
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
 * KPI row for the Access Points overview: three stat panels with counts of
 * wireless devices in each Meraki-reported status bucket. Backed by the
 * server-side `DeviceAvailabilityCounts` aggregator (todos.txt §G.20).
 */
export function apStatusKpiRow(): VizPanel[] {
  const productTypes: MerakiProductType[] = ['wireless'];
  return [
    deviceAvailabilityStat({
      title: 'APs online',
      fieldName: 'online',
      productTypes,
      thresholds: [
        { value: 0, color: 'red' },
        { value: 1, color: 'green' },
      ],
    }),
    deviceAvailabilityStat({
      title: 'APs alerting',
      fieldName: 'alerting',
      productTypes,
      thresholds: [
        { value: 0, color: 'green' },
        { value: 1, color: 'orange' },
      ],
    }),
    deviceAvailabilityStat({
      title: 'APs offline',
      fieldName: 'offline',
      productTypes,
      thresholds: [
        { value: 0, color: 'green' },
        { value: 1, color: 'red' },
      ],
    }),
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
          url: `${PLUGIN_BASE_URL}/${ROUTES.AccessPoints}/\${__value.raw:percentencode}\${__url.params}`,
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
        // String-valued `model` / `networkId` / `firmware` tiles need the
        // wildcard regex — stat's default `fields: ''` means "numeric only"
        // and silently drops string columns.
        fields: '/.*/',
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
      // `status` is a string; force-include via regex.
      fields: '/.*/',
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

// ---------------------------------------------------------------------------
// §2.1 — Org-level AP client counts table
// ---------------------------------------------------------------------------

/**
 * Org-wide table of wireless AP client counts. Uses the single-call
 * GET /organizations/{organizationId}/wireless/clients/overview/byDevice endpoint
 * so the overview page no longer fans out N per-AP requests.
 *
 * Serial column drills into the per-AP detail page.
 */
export function apClientsByDeviceTable(): VizPanel {
  const runner = oneQuery({ kind: QueryKind.WirelessApClientCounts });
  return PanelBuilders.table()
    .setTitle('AP client counts')
    .setDescription('Currently-associated clients per access point (org-wide snapshot).')
    .setData(hideColumns(runner, ['networkId']))
    .setNoValue('No AP client data available.')
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideLinks([
        {
          title: 'Open access point',
          url: `${PLUGIN_BASE_URL}/${ROUTES.AccessPoints}/\${__value.raw:percentencode}\${__url.params}`,
        },
      ]);
      b.matchFieldsWithName('online').overrideDisplayName('Online clients');
      b.matchFieldsWithName('networkName').overrideDisplayName('Network');
    })
    .build();
}

// ---------------------------------------------------------------------------
// §3.2 — Wireless packet loss by network table
// ---------------------------------------------------------------------------

/**
 * Table of wireless packet-loss metrics aggregated per network. Uses
 * GET /organizations/{organizationId}/wireless/devices/packetLoss/byNetwork.
 * Sortable by loss percentage; useful for spotting networks with RF issues.
 */
export function wirelessPacketLossByNetworkTable(): VizPanel {
  const runner = oneQuery({ kind: QueryKind.WirelessPacketLossByNetwork });
  return PanelBuilders.table()
    .setTitle('Wireless packet loss by network')
    .setDescription('Downstream / upstream packet loss aggregated per network over the selected time range.')
    .setData(hideColumns(runner, ['networkId']))
    .setNoValue('No packet loss data available.')
    .setOverrides((b) => {
      b.matchFieldsWithName('networkName').overrideDisplayName('Network');
      b.matchFieldsWithName('downstreamLossPct').overrideDisplayName('DS loss %').overrideUnit('percent');
      b.matchFieldsWithName('upstreamLossPct').overrideDisplayName('US loss %').overrideUnit('percent');
      b.matchFieldsWithName('totalLossPct').overrideDisplayName('Total loss %').overrideUnit('percent');
      b.matchFieldsWithName('downstreamTotal').overrideDisplayName('DS packets');
      b.matchFieldsWithName('downstreamLost').overrideDisplayName('DS lost');
      b.matchFieldsWithName('upstreamTotal').overrideDisplayName('US packets');
      b.matchFieldsWithName('upstreamLost').overrideDisplayName('US lost');
      b.matchFieldsWithName('totalPackets').overrideDisplayName('Total packets');
      b.matchFieldsWithName('totalLost').overrideDisplayName('Total lost');
    })
    .build();
}

// ---------------------------------------------------------------------------
// §3.2 — Wireless ethernet status table
// ---------------------------------------------------------------------------

/**
 * Table of ethernet/power status for every wireless AP. Uses
 * GET /organizations/{organizationId}/wireless/devices/ethernet/statuses.
 * Shows uplink speed, duplex mode, PoE status, and power source per AP.
 */
export function wirelessEthernetStatusTable(): VizPanel {
  const runner = oneQuery({ kind: QueryKind.WirelessDevicesEthernetStatuses });
  return PanelBuilders.table()
    .setTitle('AP ethernet statuses')
    .setDescription('Ethernet port speed, duplex, and PoE status for every access point (snapshot).')
    .setData(hideColumns(runner, ['networkId']))
    .setNoValue('No ethernet status data available.')
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideLinks([
        {
          title: 'Open access point',
          url: `${PLUGIN_BASE_URL}/${ROUTES.AccessPoints}/\${__value.raw:percentencode}\${__url.params}`,
        },
      ]);
      b.matchFieldsWithName('networkName').overrideDisplayName('Network');
      b.matchFieldsWithName('primarySpeed').overrideDisplayName('Speed');
      b.matchFieldsWithName('primaryDuplex').overrideDisplayName('Duplex');
      b.matchFieldsWithName('primaryPoe').overrideDisplayName('PoE');
      b.matchFieldsWithName('power').overrideDisplayName('Power source');
    })
    .build();
}

// ---------------------------------------------------------------------------
// §3.2 — Wireless AP CPU load timeseries
// ---------------------------------------------------------------------------

/**
 * Timeseries of per-AP CPU load (5-minute average). Uses
 * GET /organizations/{organizationId}/wireless/devices/system/cpu/load/history.
 * One series per AP; the DisplayNameFromDS is baked by the handler.
 * Time window is capped to 1 day per Meraki's API limit.
 */
export function wirelessApCpuLoadTimeseries(): VizPanel {
  return PanelBuilders.timeseries()
    .setTitle('AP CPU load')
    .setDescription('5-minute average CPU utilisation per access point. Max 1-day lookback.')
    .setData(
      oneQuery({
        kind: QueryKind.WirelessDevicesCpuLoadHistory,
        maxDataPoints: 300,
      })
    )
    .setUnit('percent')
    .setMin(0)
    .setMax(100)
    .setNoValue('No CPU load data in the selected range.')
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
 * Timeseries of per-AP memory usage (5-minute intervals). Uses
 * GET /organizations/{organizationId}/devices/system/memory/usage/history/byInterval
 * scoped to `productTypes=wireless`. One series per AP; 31-day max lookback.
 */
export function wirelessApMemoryTimeseries(): VizPanel {
  return PanelBuilders.timeseries()
    .setTitle('AP memory usage')
    .setDescription('Maximum memory usage % per access point over the selected range.')
    .setData(
      oneQuery({
        kind: QueryKind.DeviceMemoryHistory,
        productTypes: ['wireless'],
        maxDataPoints: 300,
      })
    )
    .setUnit('percent')
    .setMin(0)
    .setMax(100)
    .setNoValue('No memory usage data in the selected range.')
    .setColor({ mode: FieldColorModeId.PaletteClassic })
    .setCustomFieldConfig('lineWidth', 1)
    .setCustomFieldConfig('fillOpacity', 10)
    .setCustomFieldConfig('spanNulls', true)
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .setOption('tooltip', { mode: 'multi', sort: 'desc' } as any)
    .build();
}

// ---------------------------------------------------------------------------
// §4.4.3-1a — v0.5 MR panels
// ---------------------------------------------------------------------------

/**
 * Per-network client-count timeseries. One frame per network (see
 * handleWirelessClientCountHistory for shape). Stacked so operators can
 * eyeball the aggregate at a glance.
 *
 * When a specific SSID is passed (via `ssid`), it's smuggled through
 * `metrics[0]` — the handler reads that as an optional SSID filter.
 */
export function perSsidClientCountTimeseries(ssid?: string): VizPanel {
  return PanelBuilders.timeseries()
    .setTitle(ssid ? `Client count — SSID ${ssid}` : 'Client count')
    .setDescription(
      'Associated-client count per network over the selected time range. 7-day max lookback.'
    )
    .setData(
      oneQuery({
        kind: QueryKind.WirelessClientCountHistory,
        networkIds: ['$network'],
        metrics: ssid ? ([ssid] as unknown as SensorMetric[]) : undefined,
        maxDataPoints: 500,
      })
    )
    .setUnit('short')
    .setMin(0)
    .setNoValue('No client-count data in the selected range.')
    .setColor({ mode: FieldColorModeId.PaletteClassic })
    .setCustomFieldConfig('lineWidth', 1)
    .setCustomFieldConfig('fillOpacity', 20)
    .setCustomFieldConfig('spanNulls', true)
    .setCustomFieldConfig('stacking', { mode: 'normal', group: 'A' } as any)
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .setOption('tooltip', { mode: 'multi', sort: 'desc' } as any)
    .build();
}

/**
 * Band-split donut — reshape of the existing WirelessUsage kind. Uses the
 * stat viz in donut mode because Scenes' PanelBuilders exposes `piechart`
 * via the generic builder. The dedicated MR-band breakdown is approximated
 * via SSID usage history sliced by network; a future refinement may split
 * by band once Meraki exposes per-band usage directly.
 */
export function bandUsageSplitDonut(): VizPanel {
  return PanelBuilders.piechart()
    .setTitle('Wireless usage by network')
    .setDescription(
      'Network share of wireless throughput over the selected range. Reshape of the existing wirelessUsage kind — no new transport.'
    )
    .setData(
      oneQuery({
        kind: QueryKind.WirelessUsage,
        networkIds: ['$network'],
        maxDataPoints: 200,
      })
    )
    .setUnit('Kbits')
    .setNoValue('No usage data in the selected range.')
    .setOption('reduceOptions', {
      values: false,
      calcs: ['sum'],
      fields: '',
    } as any)
    .setOption('pieType', 'donut' as any)
    .setOption('displayLabels', ['name', 'percent'] as any)
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'right' } as any)
    .build();
}

/**
 * Org-wide AP radio/band status table. One row per AP with band2_4 / band5 /
 * band6 booleans derived from the `ssids/statuses/byDevice` response.
 */
export function perApRadioStatusTable(): VizPanel {
  const runner = oneQuery({ kind: QueryKind.DeviceRadioStatus });
  return PanelBuilders.table()
    .setTitle('AP radio status')
    .setDescription('Which radio bands (2.4 / 5 / 6 GHz) are currently broadcasting on each AP.')
    .setData(runner)
    .setNoValue('No radio-status data available.')
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideLinks([
        {
          title: 'Open access point',
          url: `${PLUGIN_BASE_URL}/${ROUTES.AccessPoints}/\${__value.raw:percentencode}\${__url.params}`,
        },
      ]);
      b.matchFieldsWithName('band2_4').overrideDisplayName('2.4 GHz');
      b.matchFieldsWithName('band5').overrideDisplayName('5 GHz');
      b.matchFieldsWithName('band6').overrideDisplayName('6 GHz');
      b.matchFieldsWithName('enabled').overrideDisplayName('SSIDs enabled');
      b.matchFieldsWithName('name').overrideDisplayName('Name');
    })
    .build();
}

/**
 * Failed-connection aggregation table. The backend emits
 * (serial, ssid, type, count) rows grouped server-side.
 */
export function failedConnectionRateTimeseries(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.WirelessFailedConnections,
    networkIds: ['$network'],
  });
  return PanelBuilders.table()
    .setTitle('Failed connections')
    .setDescription(
      'Aggregated client-connection failures in the selected range (grouped by AP / SSID / failure type).'
    )
    .setData(runner)
    .setNoValue('No failed-connection events in the selected range.')
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideDisplayName('AP');
      b.matchFieldsWithName('ssid').overrideDisplayName('SSID #');
      b.matchFieldsWithName('type').overrideDisplayName('Failure type');
      b.matchFieldsWithName('count').overrideDisplayName('Count');
    })
    .build();
}

/**
 * Per-network wireless-latency timeseries. The handler emits one frame per
 * (network, category) with an `ms` unit already configured.
 */
export function clientLatencyStatsTimeseries(): VizPanel {
  return PanelBuilders.timeseries()
    .setTitle('Wireless latency')
    .setDescription(
      'Average wireless latency per network, broken out by traffic access category when available. 7-day max lookback.'
    )
    .setData(
      oneQuery({
        kind: QueryKind.WirelessLatencyStats,
        networkIds: ['$network'],
        maxDataPoints: 500,
      })
    )
    .setUnit('ms')
    .setMin(0)
    .setNoValue('No latency samples in the selected range.')
    .setColor({ mode: FieldColorModeId.PaletteClassic })
    .setCustomFieldConfig('lineWidth', 1)
    .setCustomFieldConfig('fillOpacity', 10)
    .setCustomFieldConfig('spanNulls', true)
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .setOption('tooltip', { mode: 'multi', sort: 'desc' } as any)
    .build();
}
