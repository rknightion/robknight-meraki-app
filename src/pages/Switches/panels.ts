import { FieldColorModeId, ThresholdsMode } from '@grafana/schema';
import {
  PanelBuilders,
  SceneDataTransformer,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { deviceAvailabilityStat, switchPortsOverviewStat } from '../../scene-helpers/panels';
import { QueryKind } from '../../datasource/types';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';
import type { MerakiProductType, SensorMetric } from '../../types';

// Shared query-runner factory -------------------------------------------------
//
// This mirrors `oneQuery(...)` in `src/scene-helpers/panels.ts`. We keep a
// local copy so the Switches area doesn't depend on an internal helper in the
// shared panels module (which other agents are mutating in parallel during
// Wave 3).

interface QueryParams {
  refId?: string;
  kind: QueryKind;
  orgId?: string;
  networkIds?: string[];
  serials?: string[];
  productTypes?: MerakiProductType[];
  metrics?: SensorMetric[] | string[];
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

// Switch inventory -----------------------------------------------------------

/**
 * Table of every MS device in the selected org. Serial column drills into the
 * per-switch detail scene; mac/lat/lng columns are hidden because they're
 * rarely useful in a dense inventory view.
 */
export function switchInventoryTable(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.Devices,
    productTypes: ['switch'],
  });
  return PanelBuilders.table()
    .setTitle('Switch inventory')
    .setDescription('MS switch devices in the selected organization. Click a serial to drill in.')
    .setData(hideColumns(runner, ['lat', 'lng', 'mac']))
    .setNoValue('No switch devices found in this organization.')
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideLinks([
        {
          title: 'Open switch',
          url: `${PLUGIN_BASE_URL}/${ROUTES.Switches}/\${__value.raw:percentencode}?var-org=\${var-org:queryparam}`,
        },
      ]);
      b.matchFieldsWithName('firmware').overrideCustomFieldConfig('width', 140);
      b.matchFieldsWithName('address').overrideCustomFieldConfig('width', 260);
    })
    .build();
}

// Switch KPI row -------------------------------------------------------------

// Availability counts (fleet total, alerting, …) come from the server-side
// `DeviceAvailabilityCounts` aggregator; ports total + PoE draw come from the
// equivalent `SwitchPortsOverview` aggregator (todos.txt §G.20). Both
// replaced client-side `filterByValue+reduce` chains that crashed on current
// Grafana versions with "undefined not found in fieldMatchers".

/**
 * KPI row for the Switches overview: fleet total, ports total, PoE draw,
 * alerting count. Consumers wrap each panel in a `SceneCSSGridItem` to lay
 * out a dense row.
 */
export function switchKpiRow(): VizPanel[] {
  const productTypes: MerakiProductType[] = ['switch'];
  return [
    deviceAvailabilityStat({
      title: 'Switches total',
      fieldName: 'total',
      productTypes,
    }),
    switchPortsOverviewStat({
      title: 'Ports total',
      fieldName: 'portCount',
    }),
    switchPortsOverviewStat({
      title: 'PoE draw total',
      fieldName: 'poeTotalWatts',
      unit: 'watt',
    }),
    deviceAvailabilityStat({
      title: 'Switches alerting',
      fieldName: 'alerting',
      productTypes,
      thresholds: [
        { value: 0, color: 'green' },
        { value: 1, color: 'orange' },
      ],
    }),
  ];
}

// Port map (the centrepiece) -------------------------------------------------

/**
 * Port map for a single switch (or one member of a stack). Columns shown:
 *
 *   portId, status, duplex, speedMbps, clientCount, poePowerW, vlan,
 *   allowedVlans
 *
 * The `speedMbps` column is coloured via threshold overrides so a port
 * running at 100 Mbps stands out from a 1 Gbps neighbour at a glance. Down
 * ports render red.
 *
 * The `portId` column carries a drilldown link into the per-port detail
 * page. We use `${__data.fields.portId}` rather than `${__value.raw}` so the
 * link text matches what the row shows, regardless of any user column
 * reordering.
 */
export function switchPortMap(serial: string): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.SwitchPorts,
    serials: [serial],
  });
  return PanelBuilders.table()
    .setTitle('Port map')
    .setDescription(
      'Per-port status for this switch. Click a port ID to open packet counters and config.'
    )
    .setData(runner)
    .setNoValue('No port status reported for this switch.')
    .setOverrides((b) => {
      // Colour-coded link speed: red (down) → orange (10M) → yellow (100M) → green (1G+).
      b.matchFieldsWithName('speedMbps').overrideThresholds({
        mode: ThresholdsMode.Absolute,
        steps: [
          { value: 0, color: 'red' },
          { value: 10, color: 'orange' },
          { value: 100, color: 'yellow' },
          { value: 1000, color: 'green' },
        ],
      });
      b.matchFieldsWithName('speedMbps').overrideCustomFieldConfig('cellOptions', {
        type: 'color-background',
      } as any);
      // Drilldown on port ID — preserves the current org selection via the
      // `var-org` query-param so the per-port scene can keep the cascade.
      b.matchFieldsWithName('portId').overrideLinks([
        {
          title: 'Open port',
          url: `${PLUGIN_BASE_URL}/${ROUTES.Switches}/${encodeURIComponent(
            serial
          )}/ports/\${__value.raw:percentencode}?var-org=\${var-org:queryparam}`,
        },
      ]);
      b.matchFieldsWithName('status').overrideCustomFieldConfig('width', 100);
      b.matchFieldsWithName('duplex').overrideCustomFieldConfig('width', 100);
      b.matchFieldsWithName('speedMbps').overrideCustomFieldConfig('width', 120);
      b.matchFieldsWithName('clientCount').overrideCustomFieldConfig('width', 110);
      b.matchFieldsWithName('poePowerW').overrideCustomFieldConfig('width', 110);
      b.matchFieldsWithName('vlan').overrideCustomFieldConfig('width', 80);
    })
    .build();
}

// Per-port detail ------------------------------------------------------------

/**
 * Snapshot packet counters for one port. The backend reuses `q.Metrics[0]` as
 * the port ID for `SwitchPortPacketCounters` queries (B3 agent's dispatcher
 * decision — no dedicated `portId` field on `MerakiQuery`). Ugly but real; we
 * document it here and at the call site so anyone reading the code later
 * doesn't spend time wondering why a "metrics" slot is holding a port ID.
 *
 * Columns rendered as a wide table:
 *   desc, total, sent, recv, ratePerSecTotal, ratePerSecSent, ratePerSecRecv
 */
export function switchPortPacketCountersPanel(serial: string, portId: string): VizPanel {
  // Port ID travels through the `metrics` field — see dispatcher note above.
  const runner = new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [
      {
        refId: 'A',
        kind: QueryKind.SwitchPortPacketCounters,
        orgId: '$org',
        serials: [serial],
        metrics: [portId],
      },
    ],
  });
  return PanelBuilders.table()
    .setTitle('Packet counters')
    .setDescription(
      'Per-counter totals and derived per-second rates for this port. Snapshot only — no time series.'
    )
    .setData(runner)
    .setNoValue('No packet counters reported for this port.')
    .setOverrides((b) => {
      b.matchFieldsWithName('desc').overrideCustomFieldConfig('width', 220);
      b.matchFieldsWithName('total').overrideUnit('short');
      b.matchFieldsWithName('sent').overrideUnit('short');
      b.matchFieldsWithName('recv').overrideUnit('short');
      b.matchFieldsWithName('ratePerSecTotal').overrideUnit('cps');
      b.matchFieldsWithName('ratePerSecSent').overrideUnit('cps');
      b.matchFieldsWithName('ratePerSecRecv').overrideUnit('cps');
    })
    .build();
}

/**
 * Port configuration summary (name, type, VLAN, allowed VLANs, PoE, STP,
 * tags). Sourced from the `SwitchPortConfig` query kind, which hits the
 * per-device `/devices/{serial}/switch/ports` endpoint and is filtered
 * client-side to one row by the portId column.
 */
export function switchPortConfigPanel(serial: string, portId: string): VizPanel {
  // The backend accepts `metrics[0]` as an optional portId filter (see
  // `handleSwitchPortConfig`), so we push the filter server-side instead of
  // running a client-side `filterByValue` transform that's documented as
  // fragile in todos.txt §G.20.
  const runner = oneQuery({
    kind: QueryKind.SwitchPortConfig,
    serials: [serial],
    metrics: [portId],
  });
  return PanelBuilders.table()
    .setTitle('Port configuration')
    .setDescription('Configured port settings for this interface.')
    .setData(runner)
    .setNoValue('No configuration reported for this port.')
    .build();
}

// Per-switch overview --------------------------------------------------------

/**
 * KPI tiles for a single switch — status, model, firmware, network. Derived
 * from the Devices table filtered to one serial plus the DeviceAvailabilities
 * frame for the live online/offline status. Each stat reads one column via a
 * filterFieldsByName transform so the stat viz picks up exactly one value.
 */
export function switchOverviewKpiRow(serial: string): VizPanel[] {
  const deviceRunner = oneQuery({
    kind: QueryKind.Devices,
    serials: [serial],
    productTypes: ['switch'],
  });

  function pickFromDevices(title: string, field: string): VizPanel {
    return PanelBuilders.stat()
      .setTitle(title)
      .setData(
        new SceneDataTransformer({
          $data: deviceRunner,
          transformations: [
            {
              id: 'filterFieldsByName',
              options: { include: { names: [field] } },
            },
          ],
        })
      )
      .setNoValue('—')
      .setOption('reduceOptions', {
        values: false,
        calcs: ['lastNotNull'],
        // The Devices frame's `model` / `firmware` fields are STRINGS. Stat's
        // default `fields: ''` means "numeric only" and silently drops string
        // columns — a regex matching every field name forces the panel to
        // include the string column we just isolated via filterFieldsByName.
        fields: '/.*/',
      } as any)
      .setOption('colorMode', 'none' as any)
      .build();
  }

  const availabilityRunner = oneQuery({
    kind: QueryKind.DeviceAvailabilities,
    serials: [serial],
    productTypes: ['switch'],
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
      // `status` is a string; stat's default `fields: ''` excludes strings.
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

  // Summed client count across every port on this switch — backed by the
  // server-side `SwitchPortsOverview` aggregator (todos.txt §G.20). Previous
  // client-side `reduce` chain crashed with "undefined not found in
  // fieldMatchers" on current Grafana versions.
  const clients = switchPortsOverviewStat({
    title: 'Clients',
    fieldName: 'clientCount',
    serials: [serial],
  });

  return [
    status,
    pickFromDevices('Model', 'model'),
    pickFromDevices('Firmware', 'firmware'),
    clients,
  ];
}

// §3.1 — Switch ports by speed + usage history ---------------------------------

/**
 * Bar gauge showing active port counts per (media × speed) bucket, e.g.
 * RJ45 1000 Mbps → 48 ports, SFP 10000 Mbps → 4 ports.
 *
 * Backed by the `SwitchPortsOverviewBySpeed` kind which calls
 * GET /organizations/{organizationId}/switch/ports/overview and flattens
 * the nested byMediaAndLinkSpeed response into one row per bucket.
 */
export function switchPortsBySpeedStatPanel(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.SwitchPortsOverviewBySpeed,
  });

  return PanelBuilders.bargauge()
    .setTitle('Ports by speed')
    .setDescription('Active port counts broken down by media type and link speed.')
    .setData(runner)
    .setNoValue('No port speed data available.')
    .setOption('orientation', 'horizontal' as any)
    .setOption('reduceOptions', {
      values: true,
      calcs: ['sum'],
      fields: 'active',
    } as any)
    .setOverrides((b) => {
      b.matchFieldsWithName('active')
        .overrideColor({ mode: FieldColorModeId.PaletteClassic })
        .overrideDisplayName('${__field.labels.speed} (${__field.labels.media})');
    })
    .build();
}

/**
 * Stacked timeseries of per-switch total throughput (kilobytes sent + received
 * per interval). Each series is one switch serial labelled via Grafana's
 * native label mechanism. Backed by `SwitchPortsUsageHistory`.
 */
export function switchPortsUsageHistoryTimeseries(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.SwitchPortsUsageHistory,
  });

  return PanelBuilders.timeseries()
    .setTitle('Switch ports usage history')
    .setDescription(
      'Aggregated traffic (upstream + downstream, kilobytes) per switch device over the selected time range.'
    )
    .setData(runner)
    .setNoValue('No usage data available for the selected range.')
    .setCustomFieldConfig('stacking', { mode: 'normal' } as any)
    .setCustomFieldConfig('fillOpacity', 20)
    .setCustomFieldConfig('lineWidth', 1)
    .setOption('legend', { showLegend: true, displayMode: 'table', placement: 'bottom' } as any)
    .setOverrides((b) => {
      b.matchFieldsByQuery('A').overrideUnit('kbytes');
    })
    .build();
}
