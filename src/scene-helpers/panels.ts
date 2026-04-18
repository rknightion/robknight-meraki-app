import { FieldColorModeId, ThresholdsMode } from '@grafana/schema';
import {
  PanelBuilders,
  SceneDataTransformer,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from './datasource';
import { ALL_SENSOR_METRICS, SENSOR_METRIC_BY_ID, SensorMetricMeta } from './sensorMetrics';
import { QueryKind } from '../datasource/types';
import { PLUGIN_BASE_URL, ROUTES } from '../constants';
import type { SensorMetric } from '../types';

// Shared query-runner factories ----------------------------------------------

/** One-off Meraki query wrapped in a SceneQueryRunner. */
function oneQuery(params: {
  refId?: string;
  kind: QueryKind;
  orgId?: string;
  networkIds?: string[];
  serials?: string[];
  metrics?: SensorMetric[];
  productTypes?: string[];
  maxDataPoints?: number;
}): SceneQueryRunner {
  const {
    refId = 'A',
    kind,
    orgId,
    networkIds,
    serials,
    metrics,
    productTypes,
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
  if (metrics && metrics.length > 0) {
    query.metrics = metrics;
  }
  if (productTypes && productTypes.length > 0) {
    query.productTypes = productTypes;
  }
  return new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [query],
    ...(maxDataPoints ? { maxDataPoints } : {}),
  });
}

/**
 * Wrap a query runner in an organize transformation that drops columns by
 * name. Used by the inventory and device tables to hide low-value fields
 * (mac, lat, lng, raw) without losing them from the underlying frame.
 */
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

// Core panels ----------------------------------------------------------------

/**
 * Simple stat panel backed by a one-shot Meraki query. Pass orgID only for
 * kinds that scope to an organization; Organizations itself ignores it.
 */
export function makeStatPanel(title: string, kind: QueryKind, orgID?: string): VizPanel {
  return PanelBuilders.stat()
    .setTitle(title)
    .setData(oneQuery({ kind, orgId: orgID }))
    .build();
}

/**
 * Legacy organisations table — kept for Home. The overview scene uses
 * `orgInventoryTable()` below which includes drilldown links.
 */
export function organizationsTable(): VizPanel {
  return PanelBuilders.table()
    .setTitle('Organizations')
    .setData(oneQuery({ kind: QueryKind.Organizations }))
    .setNoValue('No organizations visible to the configured API key.')
    .build();
}

/**
 * Organisations inventory — one row per org, with a drilldown link on the
 * `name` column that opens the per-org detail scene.
 */
export function orgInventoryTable(): VizPanel {
  return PanelBuilders.table()
    .setTitle('Organizations')
    .setData(oneQuery({ kind: QueryKind.Organizations }))
    .setNoValue('No organizations visible to the configured API key.')
    .setOverrides((b) => {
      b.matchFieldsWithName('name').overrideLinks([
        {
          title: 'Open organization',
          url: `${PLUGIN_BASE_URL}/${ROUTES.Organizations}/\${__data.fields.id:percentencode}`,
        },
      ]);
      b.matchFieldsWithName('url').overrideCustomFieldConfig('cellOptions', {
        type: 'auto' as any,
      });
    })
    .build();
}

// Sensor overview panels -----------------------------------------------------

/**
 * One-metric overview card. Backend returns one frame per reporting sensor
 * (labels: serial, metric, network_id, network_name). The timeseries viz
 * uses those labels natively for legend / series grouping — no client-side
 * transform needed.
 */
export function sensorMetricCard(meta: SensorMetricMeta): VizPanel {
  if (meta.discrete) {
    return sensorDiscreteStateCard(meta);
  }
  // Battery (and IAQ on some firmwares) reports infrequently — every few
  // hours at most. Without `spanNulls`, each individual reading renders as a
  // lone dot with no line to anchor it against. Allowing the line to bridge
  // the gaps between readings turns those into a readable trace. Setting it
  // unconditionally is safe for dense metrics too: with ~1 sample per
  // minute there's no gap to bridge.
  const builder = PanelBuilders.timeseries()
    .setTitle(meta.label)
    .setDescription(`${meta.label} across all reporting sensors in the selected networks.`)
    .setData(
      oneQuery({
        kind: QueryKind.SensorReadingsHistory,
        networkIds: ['$network'],
        metrics: [meta.id],
        maxDataPoints: 500,
      })
    )
    .setNoValue('No sensors reporting.')
    .setColor({ mode: FieldColorModeId.PaletteClassic })
    .setCustomFieldConfig('lineWidth', 1)
    .setCustomFieldConfig('fillOpacity', 10)
    .setCustomFieldConfig('spanNulls', true)
    .setCustomFieldConfig('showPoints', 'auto' as any)
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .setOption('tooltip', { mode: 'multi', sort: 'desc' } as any);

  if (meta.unit) {
    builder.setUnit(meta.unit);
  }
  if (typeof meta.min === 'number') {
    builder.setMin(meta.min);
  }
  if (typeof meta.max === 'number') {
    builder.setMax(meta.max);
  }
  return builder.build();
}

/**
 * Discrete (door / water) state-over-time panel. State timeline renders the
 * 0/1 samples we emit for these metrics as coloured bars.
 */
function sensorDiscreteStateCard(meta: SensorMetricMeta): VizPanel {
  return PanelBuilders.statetimeline()
    .setTitle(`${meta.label} events`)
    .setDescription(`${meta.label} state transitions across the selected networks.`)
    .setData(
      oneQuery({
        kind: QueryKind.SensorReadingsHistory,
        networkIds: ['$network'],
        metrics: [meta.id],
        maxDataPoints: 2000,
      })
    )
    .setNoValue('No events in the selected range.')
    .setMappings([
      {
        type: 'value' as any,
        options: {
          '0': {
            text: meta.id === 'door' ? 'Closed' : 'Dry',
            color: 'green',
            index: 0,
          },
          '1': {
            text: meta.id === 'door' ? 'Open' : 'Water detected',
            color: 'red',
            index: 1,
          },
        },
      },
    ])
    .build();
}

/**
 * Build a single KPI stat panel from the server-side `SensorAlertSummary`
 * frame. Each panel applies an `organize` transform that keeps only the
 * named field — that way the stat viz picks up exactly one number without
 * having to fight a filterByValue/reduce chain.
 */
function alertStat(
  title: string,
  field: 'sensorsReporting' | 'doorsOpen' | 'waterDetected' | 'lowBattery',
  thresholds: Array<{ value: number; color: string }>
): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.SensorAlertSummary,
    networkIds: ['$network'],
  });

  const excludeByName: Record<string, boolean> = {
    sensorsReporting: field !== 'sensorsReporting',
    doorsOpen: field !== 'doorsOpen',
    waterDetected: field !== 'waterDetected',
    lowBattery: field !== 'lowBattery',
  };

  const builder = PanelBuilders.stat()
    .setTitle(title)
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
    .setOption('colorMode', 'value' as any);

  if (thresholds.length > 0) {
    builder
      .setColor({ mode: FieldColorModeId.Thresholds })
      .setThresholds({
        mode: ThresholdsMode.Absolute,
        steps: thresholds.map((t, i) => ({ value: i === 0 ? (null as unknown as number) : t.value, color: t.color })),
      });
  }

  return builder.build();
}

/**
 * KPI row for the sensors overview — four stat panels backed by a single
 * `SensorAlertSummary` query kind. The aggregation runs server-side in Go
 * so the numbers are predictable and don't depend on client-side
 * transform schema quirks.
 */
export function sensorKpiRow(): VizPanel[] {
  return [
    alertStat('Sensors reporting', 'sensorsReporting', [
      { value: 0, color: 'red' },
      { value: 1, color: 'green' },
    ]),
    alertStat('Doors open', 'doorsOpen', [
      { value: 0, color: 'green' },
      { value: 1, color: 'orange' },
    ]),
    alertStat('Water detected', 'waterDetected', [
      { value: 0, color: 'green' },
      { value: 1, color: 'red' },
    ]),
    alertStat('Low battery (≤ 20%)', 'lowBattery', [
      { value: 0, color: 'green' },
      { value: 1, color: 'orange' },
    ]),
  ];
}

/**
 * Sensor inventory — one row per MT device in the selected org. The `serial`
 * column is a link into the per-sensor detail scene.
 */
export function sensorInventoryTable(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.Devices,
    productTypes: ['sensor'],
  });
  return PanelBuilders.table()
    .setTitle('Sensor inventory')
    .setDescription('MT sensor devices in the selected organization. Click a serial to drill in.')
    .setData(hideColumns(runner, ['lat', 'lng', 'mac']))
    .setNoValue('No sensor devices found in this organization.')
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideLinks([
        {
          title: 'Open sensor',
          url: `${PLUGIN_BASE_URL}/${ROUTES.Sensors}/\${__value.raw:percentencode}?var-org=\${var-org:queryparam}`,
        },
      ]);
      b.matchFieldsWithName('firmware').overrideCustomFieldConfig('width', 140);
      b.matchFieldsWithName('address').overrideCustomFieldConfig('width', 260);
    })
    .build();
}

// Sensor detail panels -------------------------------------------------------

/**
 * Single-sensor timeseries for one metric. Used on the sensor detail page —
 * stack one of these per metric type to build the full device view.
 */
export function sensorDetailMetricPanel(serial: string, meta: SensorMetricMeta): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.SensorReadingsHistory,
    serials: [serial],
    metrics: [meta.id],
    maxDataPoints: 1000,
  });

  if (meta.discrete) {
    return PanelBuilders.statetimeline()
      .setTitle(`${meta.label} events`)
      .setData(runner)
      .setNoValue('No events in the selected range.')
      .setMappings([
        {
          type: 'value' as any,
          options: {
            '0': {
              text: meta.id === 'door' ? 'Closed' : 'Dry',
              color: 'green',
              index: 0,
            },
            '1': {
              text: meta.id === 'door' ? 'Open' : 'Water detected',
              color: 'red',
              index: 1,
            },
          },
        },
      ])
      .build();
  }

  const builder = PanelBuilders.timeseries()
    .setTitle(meta.label)
    .setData(runner)
    .setNoValue('No data in the selected range.')
    .setColor({ mode: FieldColorModeId.PaletteClassic })
    .setCustomFieldConfig('lineWidth', 2)
    .setCustomFieldConfig('fillOpacity', 15)
    .setCustomFieldConfig('spanNulls', true)
    .setCustomFieldConfig('showPoints', 'auto' as any)
    .setOption('legend', { showLegend: false } as any)
    .setOption('tooltip', { mode: 'single' } as any);
  if (meta.unit) {
    builder.setUnit(meta.unit);
  }
  if (typeof meta.min === 'number') {
    builder.setMin(meta.min);
  }
  if (typeof meta.max === 'number') {
    builder.setMax(meta.max);
  }
  return builder.build();
}

/**
 * "Last reading" table filtered to one sensor — shows every metric that
 * sensor currently reports, in one compact panel for the detail page header.
 */
export function sensorDetailLatestReadings(serial: string): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.SensorReadingsLatest,
    serials: [serial],
  });
  return PanelBuilders.table()
    .setTitle('Latest readings')
    .setData(hideColumns(runner, ['raw', 'network_id']))
    .setNoValue('This sensor has not reported recently.')
    .setOverrides((b) => {
      b.matchFieldsWithName('ts').overrideCustomFieldConfig('width', 200);
    })
    .build();
}

/** Every metric we know about, with metadata — handy for building detail stacks. */
export const SENSOR_OVERVIEW_METRICS: SensorMetricMeta[] = ALL_SENSOR_METRICS;

// Organization detail panels -------------------------------------------------

/**
 * Per-org KPI row for the detail page. Each stat is its own small query so
 * they populate independently and failure of one doesn't blank the others.
 */
export function orgDetailKpiRow(orgId: string): VizPanel[] {
  const devicesRunner = oneQuery({ kind: QueryKind.Devices, orgId });
  const networksRunner = oneQuery({ kind: QueryKind.Networks, orgId });
  const statusRunner = oneQuery({ kind: QueryKind.DeviceStatusOverview, orgId });

  const networkCount = PanelBuilders.stat()
    .setTitle('Networks')
    .setData(
      new SceneDataTransformer({
        $data: networksRunner,
        transformations: [
          {
            id: 'reduce',
            options: { reducers: ['count'], fields: 'id', mode: 'reduceFields', includeTimeField: false },
          },
        ],
      })
    )
    .build();

  const deviceCount = PanelBuilders.stat()
    .setTitle('Devices')
    .setData(
      new SceneDataTransformer({
        $data: devicesRunner,
        transformations: [
          {
            id: 'reduce',
            options: { reducers: ['count'], fields: 'serial', mode: 'reduceFields', includeTimeField: false },
          },
        ],
      })
    )
    .build();

  const onlineCount = PanelBuilders.stat()
    .setTitle('Online')
    .setData(
      new SceneDataTransformer({
        $data: statusRunner,
        transformations: [
          {
            id: 'filterByValue',
            options: {
              filters: [
                {
                  fieldName: 'status',
                  config: { id: 'equal', options: { value: 'online' } },
                },
              ],
              type: 'include',
              match: 'all',
            },
          },
          {
            id: 'reduce',
            options: { reducers: ['sum'], fields: 'count', mode: 'reduceFields', includeTimeField: false },
          },
        ],
      })
    )
    .setColor({ mode: FieldColorModeId.Thresholds })
    .setThresholds({
      mode: ThresholdsMode.Absolute,
      steps: [
        { value: 0, color: 'red' },
        { value: 1, color: 'green' },
      ],
    })
    .build();

  const alertingCount = PanelBuilders.stat()
    .setTitle('Alerting')
    .setData(
      new SceneDataTransformer({
        $data: statusRunner,
        transformations: [
          {
            id: 'filterByValue',
            options: {
              filters: [
                {
                  fieldName: 'status',
                  config: { id: 'equal', options: { value: 'alerting' } },
                },
              ],
              type: 'include',
              match: 'all',
            },
          },
          {
            id: 'reduce',
            options: { reducers: ['sum'], fields: 'count', mode: 'reduceFields', includeTimeField: false },
          },
        ],
      })
    )
    .setColor({ mode: FieldColorModeId.Thresholds })
    .setThresholds({
      mode: ThresholdsMode.Absolute,
      steps: [
        { value: 0, color: 'green' },
        { value: 1, color: 'orange' },
      ],
    })
    .build();

  return [networkCount, deviceCount, onlineCount, alertingCount];
}

/**
 * Per-org device status donut — swap-in for the old small stat panel on the
 * home page. Pie chart is more self-explanatory with four status buckets.
 */
export function orgDeviceStatusDonut(orgId: string): VizPanel {
  return PanelBuilders.piechart()
    .setTitle('Device status')
    .setData(oneQuery({ kind: QueryKind.DeviceStatusOverview, orgId }))
    .setOption('pieType', 'donut' as any)
    .setOption('legend', { displayMode: 'list', placement: 'right', showLegend: true } as any)
    .setOption('reduceOptions', {
      values: true,
      calcs: ['lastNotNull'],
      fields: '/^count$/',
    } as any)
    .build();
}

/** Networks table scoped to one org. */
export function orgNetworksTable(orgId: string): VizPanel {
  const runner = oneQuery({ kind: QueryKind.Networks, orgId });
  return PanelBuilders.table()
    .setTitle('Networks')
    .setData(hideColumns(runner, ['organizationId']))
    .setNoValue('No networks in this organization.')
    .setOverrides((b) => {
      b.matchFieldsWithName('name').overrideCustomFieldConfig('width', 220);
    })
    .build();
}

/**
 * Devices table scoped to one org. `serial` drills into the per-family
 * detail page derived from the row's own productType — the backend emits
 * a `drilldownUrl` column for every row, so MR serials route to the
 * access-point page, MS to switches, MV to cameras, etc., without any
 * frontend template branching.
 *
 * The `drilldownUrl` column is hidden from the operator-facing view; it's
 * consumed only by the serial-column link.
 */
export function orgDevicesTable(orgId: string): VizPanel {
  const runner = oneQuery({ kind: QueryKind.Devices, orgId });
  return PanelBuilders.table()
    .setTitle('Devices')
    .setData(hideColumns(runner, ['mac', 'lat', 'lng', 'drilldownUrl']))
    .setNoValue('No devices in this organization.')
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideLinks([
        {
          title: 'Open device',
          url: '${__data.fields.drilldownUrl}',
        },
      ]);
    })
    .build();
}

/** Re-export for scenes that want to iterate over every metric. */
export { ALL_SENSOR_METRICS, SENSOR_METRIC_BY_ID };
