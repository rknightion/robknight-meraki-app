import { FieldColorModeId, ThresholdsMode } from '@grafana/schema';
import {
  PanelBuilders,
  SceneDataTransformer,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
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

/**
 * Count-of-rows stat driven by the `DeviceAvailabilities` frame filtered to
 * `productTypes=['switch']`. Mirrors the approach taken by the Access Points
 * KPI row — a client-side filterByValue+reduce chain is reliable for the
 * "count rows matching a single status value" case (todos.txt §G.20 only
 * called out the reducer quirks for mixed numeric reducers).
 */
function switchCountStat(
  title: string,
  status: 'online' | 'alerting' | 'offline' | 'dormant',
  thresholds: Array<{ value: number; color: string }>
): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.DeviceAvailabilities,
    productTypes: ['switch'],
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
 * "Total switches" stat — the length of the devices-filtered-to-switch frame.
 * Uses the Devices kind rather than DeviceAvailabilities so we include
 * dormant/offline devices in the fleet count.
 */
function switchTotalStat(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.Devices,
    productTypes: ['switch'],
  });
  return PanelBuilders.stat()
    .setTitle('Switches total')
    .setData(
      new SceneDataTransformer({
        $data: runner,
        transformations: [
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
    .setOption('colorMode', 'none' as any)
    .build();
}

/**
 * "Ports total" stat — backed by the SwitchPorts kind which emits one row
 * per port across every switch in the org. A straight count of rows in the
 * `portId` column tells us how many ports exist across the estate.
 */
function switchPortsTotalStat(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.SwitchPorts,
  });
  return PanelBuilders.stat()
    .setTitle('Ports total')
    .setData(
      new SceneDataTransformer({
        $data: runner,
        transformations: [
          {
            id: 'reduce',
            options: {
              reducers: ['count'],
              fields: 'portId',
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
    .setOption('colorMode', 'none' as any)
    .build();
}

/**
 * "PoE draw total" stat — summed `poePowerW` across every port in the org.
 * Shown in watts. When the backend didn't include the `poePowerW` column
 * (older firmwares) the reducer falls back to 0 which is fine.
 */
function switchPoeTotalStat(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.SwitchPorts,
  });
  return PanelBuilders.stat()
    .setTitle('PoE draw total')
    .setUnit('watt')
    .setData(
      new SceneDataTransformer({
        $data: runner,
        transformations: [
          {
            id: 'reduce',
            options: {
              reducers: ['sum'],
              fields: 'poePowerW',
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
    .setOption('colorMode', 'none' as any)
    .build();
}

/**
 * KPI row for the Switches overview: fleet total, ports total, PoE draw,
 * alerting count. Consumers wrap each panel in a `SceneCSSGridItem` to lay
 * out a dense row.
 */
export function switchKpiRow(): VizPanel[] {
  return [
    switchTotalStat(),
    switchPortsTotalStat(),
    switchPoeTotalStat(),
    switchCountStat('Switches alerting', 'alerting', [
      { value: 0, color: 'green' },
      { value: 1, color: 'orange' },
    ]),
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
  const runner = oneQuery({
    kind: QueryKind.SwitchPortConfig,
    serials: [serial],
  });
  return PanelBuilders.table()
    .setTitle('Port configuration')
    .setDescription('Configured port settings for this interface.')
    .setData(
      new SceneDataTransformer({
        $data: runner,
        transformations: [
          // Scope the full port-config table to just the selected port; this
          // keeps the handler simple (emits one row per port) without needing
          // a dedicated "single port" kind.
          {
            id: 'filterByValue',
            options: {
              filters: [
                {
                  fieldName: 'portId',
                  config: { id: 'equal', options: { value: portId } },
                },
              ],
              type: 'include',
              match: 'all',
            },
          },
        ],
      })
    )
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
        fields: '',
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

  // Summed client count across every port on this switch — derived from the
  // SwitchPorts frame (one row per port). Gives a rough "how busy is this
  // switch" number without needing a dedicated aggregate kind.
  const portsRunner = oneQuery({
    kind: QueryKind.SwitchPorts,
    serials: [serial],
  });
  const clients = PanelBuilders.stat()
    .setTitle('Clients')
    .setData(
      new SceneDataTransformer({
        $data: portsRunner,
        transformations: [
          {
            id: 'reduce',
            options: {
              reducers: ['sum'],
              fields: 'clientCount',
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
    .setOption('colorMode', 'none' as any)
    .build();

  return [
    status,
    pickFromDevices('Model', 'model'),
    pickFromDevices('Firmware', 'firmware'),
    clients,
  ];
}
