import { FieldColorModeId, ThresholdsMode } from '@grafana/schema';
import {
  PanelBuilders,
  SceneDataTransformer,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { deviceAvailabilityStat } from '../../scene-helpers/panels';
import { QueryKind } from '../../datasource/types';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';
import type { MerakiProductType } from '../../types';

// Shared query-runner factory -------------------------------------------------
//
// Local copy of the `oneQuery` helper — mirrors the AP / MS / MV copies. We
// keep a local version so the Cellular Gateways area doesn't depend on an
// internal helper in the shared panels module.

interface QueryParams {
  refId?: string;
  kind: QueryKind;
  orgId?: string;
  networkIds?: string[];
  serials?: string[];
  productTypes?: MerakiProductType[];
  metrics?: string[];
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

// MG KPI row ----------------------------------------------------------------

/**
 * KPI row for the Cellular Gateways overview: three stat panels with counts
 * of MG devices in each Meraki-reported status bucket. Server-side
 * aggregation via `DeviceAvailabilityCounts` (todos.txt §G.20).
 */
export function mgStatusKpiRow(): VizPanel[] {
  const productTypes: MerakiProductType[] = ['cellularGateway'];
  return [
    deviceAvailabilityStat({
      title: 'Gateways online',
      fieldName: 'online',
      productTypes,
      thresholds: [
        { value: 0, color: 'red' },
        { value: 1, color: 'green' },
      ],
    }),
    deviceAvailabilityStat({
      title: 'Gateways alerting',
      fieldName: 'alerting',
      productTypes,
      thresholds: [
        { value: 0, color: 'green' },
        { value: 1, color: 'orange' },
      ],
    }),
    deviceAvailabilityStat({
      title: 'Gateways offline',
      fieldName: 'offline',
      productTypes,
      thresholds: [
        { value: 0, color: 'green' },
        { value: 1, color: 'red' },
      ],
    }),
  ];
}

// Inventory + uplink fleet tables -------------------------------------------

/**
 * Table of every MG device in the selected org. Serial column drills into
 * the per-gateway detail scene; mac/lat/lng columns are hidden because
 * they're rarely useful in a dense inventory view.
 */
export function mgInventoryTable(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.Devices,
    productTypes: ['cellularGateway'],
  });
  return PanelBuilders.table()
    .setTitle('Gateway inventory')
    .setDescription('MG cellular gateway devices in the selected organization. Click a serial to drill in.')
    .setData(hideColumns(runner, ['lat', 'lng', 'mac', 'drilldownUrl']))
    .setNoValue('No cellular gateway devices found in this organization.')
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideLinks([
        {
          title: 'Open gateway',
          url: `${PLUGIN_BASE_URL}/${ROUTES.CellularGateways}/\${__value.raw:percentencode}?var-org=\${var-org:queryparam}`,
        },
      ]);
      b.matchFieldsWithName('firmware').overrideCustomFieldConfig('width', 140);
      b.matchFieldsWithName('address').overrideCustomFieldConfig('width', 260);
    })
    .build();
}

/**
 * Fleet-wide uplink status table. One row per `(serial, interface)` —
 * surface columns are trimmed to the fields operators actually read when
 * triaging a site: serial, interface, status, provider, publicIp, signal
 * levels. Drilldown on serial routes via the backend-emitted `drilldownUrl`
 * column.
 */
export function mgUplinkFleetTable(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.MgUplinks,
  });
  return PanelBuilders.table()
    .setTitle('Uplink fleet')
    .setDescription('Per-gateway cellular uplink status across the selected organization.')
    .setData(
      hideColumns(runner, [
        // Hide config detail by default; surfaces are available in the
        // per-device Uplink tab when the user drills in.
        'iccid',
        'apn',
        'signalType',
        'connectionType',
        'dns1',
        'dns2',
        'lastReportedAt',
        'model',
        'networkId',
        'drilldownUrl',
      ])
    )
    .setNoValue('No cellular gateways reporting uplink state in this organization.')
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideLinks([
        {
          title: 'Open gateway',
          url: '${__data.fields.drilldownUrl}',
        },
      ]);
      b.matchFieldsWithName('status').overrideMappings([
        {
          type: 'value' as any,
          options: {
            active: { color: 'green', index: 0, text: 'Active' },
            ready: { color: 'green', index: 1, text: 'Ready' },
            connecting: { color: 'orange', index: 2, text: 'Connecting' },
            notConnected: { color: 'red', index: 3, text: 'Not connected' },
            failed: { color: 'red', index: 4, text: 'Failed' },
          },
        },
      ]);
      b.matchFieldsWithName('status').overrideCustomFieldConfig('cellOptions', {
        type: 'color-background',
      } as any);
      b.matchFieldsWithName('rsrpDb').overrideUnit('dB');
      b.matchFieldsWithName('rsrqDb').overrideUnit('dB');
    })
    .build();
}

/**
 * Bar chart of per-gateway RSRP signal strength. Useful at a glance to spot
 * sites with marginal cell coverage — red bars below -110 dBm.
 */
export function mgSignalBarChart(): VizPanel {
  return PanelBuilders.barchart()
    .setTitle('RSRP signal strength')
    .setDescription('Per-gateway RSRP (dBm) across the selected organization. Red below -110.')
    .setData(
      oneQuery({
        kind: QueryKind.MgUplinks,
      })
    )
    .setUnit('dB')
    .setNoValue('No gateways reporting signal strength.')
    .setOption('legend', { showLegend: false } as any)
    .setOption('xTickLabelRotation', -45 as any)
    .setOption('xField', 'serial' as any)
    .setColor({ mode: FieldColorModeId.Thresholds })
    .setThresholds({
      mode: ThresholdsMode.Absolute,
      steps: [
        { value: null as unknown as number, color: 'red' },
        { value: -110, color: 'orange' },
        { value: -100, color: 'green' },
      ],
    })
    .build();
}

// Per-gateway panels --------------------------------------------------------

/**
 * Per-device uplink detail table — same source as the fleet table but
 * filtered to one serial. Shows the full column set (APN, ICCID, signal
 * types) since users are already at the device detail level.
 */
export function mgUplinkTable(serial: string): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.MgUplinks,
    serials: [serial],
  });
  return PanelBuilders.table()
    .setTitle('Uplink detail')
    .setDescription('Cellular uplink status for this gateway.')
    .setData(hideColumns(runner, ['drilldownUrl', 'model', 'networkId']))
    .setNoValue('No uplink status reported for this gateway.')
    .setOverrides((b) => {
      b.matchFieldsWithName('rsrpDb').overrideUnit('dB');
      b.matchFieldsWithName('rsrqDb').overrideUnit('dB');
      b.matchFieldsWithName('publicIp').overrideCustomFieldConfig('width', 140);
    })
    .build();
}

/**
 * Signal-strength gauge for one gateway, keyed on either RSRP or RSRQ. Uses
 * a `filterFieldsByName` transform to narrow the frame to the requested
 * column so the gauge viz picks up exactly one number. Thresholds match the
 * common mobile-ops convention: -110 dB as the "marginal" floor, -100 dB as
 * "good enough".
 */
export function mgSignalGauge(serial: string, metric: 'rsrpDb' | 'rsrqDb'): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.MgUplinks,
    serials: [serial],
  });

  const title = metric === 'rsrpDb' ? 'RSRP signal' : 'RSRQ quality';
  return PanelBuilders.gauge()
    .setTitle(title)
    .setData(
      new SceneDataTransformer({
        $data: runner,
        transformations: [
          {
            id: 'filterFieldsByName',
            options: { include: { names: [metric] } },
          },
        ],
      })
    )
    .setUnit('dB')
    .setMin(-130)
    .setMax(-60)
    .setNoValue('—')
    .setOption('reduceOptions', {
      values: false,
      calcs: ['lastNotNull'],
      fields: '',
    } as any)
    .setColor({ mode: FieldColorModeId.Thresholds })
    .setThresholds({
      mode: ThresholdsMode.Absolute,
      steps: [
        { value: null as unknown as number, color: 'red' },
        { value: -110, color: 'orange' },
        { value: -100, color: 'green' },
      ],
    })
    .build();
}

/**
 * Port-forwarding rules for a single gateway. Renders as a flat table; the
 * backend collapses the per-rule allowed-IPs list into a comma-joined
 * string so Grafana's table viz can display it cleanly.
 */
export function mgPortForwardingTable(serial: string): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.MgPortForwarding,
    serials: [serial],
  });
  return PanelBuilders.table()
    .setTitle('Port forwarding rules')
    .setDescription('Inbound port-forwarding rules configured on this gateway.')
    .setData(runner)
    .setNoValue('No port-forwarding rules configured on this gateway.')
    .setOverrides((b) => {
      b.matchFieldsWithName('name').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('protocol').overrideCustomFieldConfig('width', 90);
      b.matchFieldsWithName('publicPort').overrideCustomFieldConfig('width', 110);
      b.matchFieldsWithName('localPort').overrideCustomFieldConfig('width', 110);
      b.matchFieldsWithName('lanIp').overrideCustomFieldConfig('width', 140);
    })
    .build();
}

/**
 * Combined fixed-IP assignments + reserved-range table. The backend
 * flattens both into one frame with a `kind` discriminator column so a
 * single panel can show both with filters.
 */
export function mgLanPanel(serial: string): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.MgLan,
    serials: [serial],
  });
  return PanelBuilders.table()
    .setTitle('LAN configuration')
    .setDescription('Fixed-IP assignments and reserved-IP ranges configured on this gateway.')
    .setData(runner)
    .setNoValue('No LAN configuration reported for this gateway.')
    .setOverrides((b) => {
      b.matchFieldsWithName('kind').overrideCustomFieldConfig('width', 100);
      b.matchFieldsWithName('identifier').overrideCustomFieldConfig('width', 220);
      b.matchFieldsWithName('ip').overrideCustomFieldConfig('width', 140);
    })
    .build();
}

/**
 * Connectivity-monitoring destinations for a single network — a short list
 * of probe targets the MG uses to validate the uplink is usable. Bound to
 * `$network` so the overview scene can surface it without a per-device
 * cascade.
 */
export function mgConnectivityPanel(networkId: string): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.MgConnectivity,
    networkIds: [networkId],
  });
  return PanelBuilders.table()
    .setTitle('Connectivity monitoring')
    .setDescription('Connectivity probe targets configured for this network.')
    .setData(runner)
    .setNoValue('No connectivity-monitoring destinations configured for this network.')
    .setOverrides((b) => {
      b.matchFieldsWithName('ip').overrideCustomFieldConfig('width', 160);
      b.matchFieldsWithName('isDefault').overrideCustomFieldConfig('width', 100);
    })
    .build();
}

// Per-gateway overview KPIs -------------------------------------------------

/**
 * KPI tiles for one gateway — status, model, firmware, network. Mirrors
 * the AP and Camera detail KPI rows: each tile is a stat panel reading one
 * column out of the single-row Devices frame (or the availability frame
 * for the live status tile) via a `filterFieldsByName` transform.
 */
export function mgOverviewKpiRow(serial: string): VizPanel[] {
  const deviceRunner = oneQuery({
    kind: QueryKind.Devices,
    serials: [serial],
    productTypes: ['cellularGateway'],
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
        // String fields need the wildcard regex — `fields: ''` is numeric-only.
        fields: '/.*/',
      } as any)
      .setOption('colorMode', 'none' as any)
      .build();
  }

  const availabilityRunner = oneQuery({
    kind: QueryKind.DeviceAvailabilities,
    serials: [serial],
    productTypes: ['cellularGateway'],
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

  return [
    status,
    pickFromDevices('Model', 'model'),
    pickFromDevices('Firmware', 'firmware'),
    pickFromDevices('Network', 'networkId'),
  ];
}
