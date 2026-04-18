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

// Shared query-runner factory --------------------------------------------------
//
// Mirrors `oneQuery(...)` in `src/scene-helpers/panels.ts`. We keep a local
// copy so the Appliances area doesn't depend on an internal helper in the
// shared panels module (which other agents are mutating in parallel during
// Wave 3) — matches the pattern set by AccessPoints/panels.ts and
// Switches/panels.ts.

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

// MX status KPI row ----------------------------------------------------------

/**
 * Count-of-rows stat driven by the `DeviceAvailabilities` frame filtered to
 * `productTypes=['appliance']`. Mirrors the approach taken by the Access
 * Points KPI row — a client-side filterByValue+reduce chain is reliable for
 * the "count rows matching a single status value" case (todos.txt §G.20 only
 * called out the reducer quirks for mixed numeric reducers).
 */
function availabilityStat(
  title: string,
  status: 'online' | 'alerting' | 'offline' | 'dormant',
  thresholds: Array<{ value: number; color: string }>
): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.DeviceAvailabilities,
    productTypes: ['appliance'],
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
 * KPI row for the Appliances overview: three stat panels with counts of
 * MX devices in each Meraki-reported status bucket. Consumers wrap each
 * panel in a `SceneCSSGridItem` to lay out a dense row.
 */
export function mxStatusKpiRow(): VizPanel[] {
  return [
    availabilityStat('Appliances online', 'online', [
      { value: 0, color: 'red' },
      { value: 1, color: 'green' },
    ]),
    availabilityStat('Appliances alerting', 'alerting', [
      { value: 0, color: 'green' },
      { value: 1, color: 'orange' },
    ]),
    availabilityStat('Appliances offline', 'offline', [
      { value: 0, color: 'green' },
      { value: 1, color: 'red' },
    ]),
  ];
}

// Uplinks overview KPI row ---------------------------------------------------

/**
 * Build one KPI stat from the wide `ApplianceUplinksOverview` frame (one row
 * with five int64 counts). We keep only the named field via a
 * `filterFieldsByName` transform so the stat viz picks up exactly one value
 * — mirrors `alertStat` in `src/pages/Alerts/panels.ts`.
 *
 * Thresholds are optional: failed / notConnected panels get red-above-zero
 * colour ramps; active / ready stay neutral because high counts are good.
 */
function uplinksOverviewStat(
  title: string,
  field: 'active' | 'ready' | 'failed' | 'notConnected',
  thresholds?: Array<{ value: number; color: string }>
): VizPanel {
  const runner = oneQuery({ kind: QueryKind.ApplianceUplinksOverview });

  const builder = PanelBuilders.stat()
    .setTitle(title)
    .setData(
      new SceneDataTransformer({
        $data: runner,
        transformations: [
          {
            id: 'filterFieldsByName',
            options: { include: { names: [field] } },
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

  if (thresholds && thresholds.length > 0) {
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
 * KPI row for uplink health: active / ready / failed / not-connected counts.
 * Backed by the server-side `ApplianceUplinksOverview` kind which emits a
 * single wide frame (one column per status) — matches the
 * `sensorAlertSummary` / `alertsOverview` pattern called out in §G.20.
 */
export function applianceUplinksOverviewRow(): VizPanel[] {
  return [
    uplinksOverviewStat('Active', 'active'),
    uplinksOverviewStat('Ready', 'ready'),
    uplinksOverviewStat('Failed', 'failed', [
      { value: 0, color: 'green' },
      { value: 1, color: 'red' },
    ]),
    uplinksOverviewStat('Not connected', 'notConnected', [
      { value: 0, color: 'green' },
      { value: 1, color: 'orange' },
    ]),
  ];
}

// Uplink status table --------------------------------------------------------

/**
 * Table of every MX uplink in the selected org (one row per appliance per
 * interface). Serial column drills into the per-appliance detail page; the
 * status column gets a value-mapping override that renders as a coloured
 * background cell (green / blue / red / orange). Cellular-only columns
 * (iccid, provider, signalType, rsrp, rsrq, apn) are hidden by default — the
 * frame still carries them for non-cellular uplinks where they stay empty,
 * which adds a lot of clutter on mostly-wired fleets.
 *
 * Optional `serials` filter scopes the table to one appliance for the
 * per-device Overview tab. When omitted the panel shows all appliances.
 */
export function applianceUplinkStatusTable(serial?: string): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.ApplianceUplinkStatuses,
    serials: serial ? [serial] : undefined,
  });

  // Drop low-signal cellular-only columns so mostly-wired fleets aren't
  // buried in empty cells. The frame still carries them so a future override
  // can re-introduce them (e.g. a cellular-focused dashboard).
  const organized = hideColumns(runner, [
    'iccid',
    'provider',
    'signalType',
    'apn',
    'rsrp',
    'rsrq',
    'drilldownUrl',
  ]);

  return PanelBuilders.table()
    .setTitle('Uplink status')
    .setDescription(
      'Per-uplink status for every MX in the selected organization. Click a serial to drill in.'
    )
    .setData(organized)
    .setNoValue('No uplink status reported for the selected appliances.')
    .setMappings([
      {
        type: 'value' as any,
        options: {
          active: { color: 'green', index: 0, text: 'Active' },
          ready: { color: 'blue', index: 1, text: 'Ready' },
          failed: { color: 'red', index: 2, text: 'Failed' },
          // "not connected" comes off the wire verbatim as a two-word string;
          // the mapping key is matched literally. Grey would be ideal but
          // Grafana's colour palette doesn't include a neutral "grey" token —
          // orange reads as a softer warning next to a red "failed" peer.
          'not connected': { color: 'orange', index: 3, text: 'Not connected' },
        },
      },
    ])
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideLinks([
        {
          title: 'Open appliance',
          url: `${PLUGIN_BASE_URL}/${ROUTES.Appliances}/\${__value.raw:percentencode}?var-org=\${var-org:queryparam}`,
        },
      ]);
      b.matchFieldsWithName('status').overrideCustomFieldConfig('cellOptions', {
        type: 'color-background',
      } as any);
      b.matchFieldsWithName('status').overrideCustomFieldConfig('width', 120);
      b.matchFieldsWithName('interface').overrideCustomFieldConfig('width', 110);
      b.matchFieldsWithName('ip').overrideCustomFieldConfig('width', 140);
      b.matchFieldsWithName('publicIp').overrideCustomFieldConfig('width', 150);
      b.matchFieldsWithName('lastReportedAt').overrideCustomFieldConfig('width', 180);
    })
    .build();
}

// Appliance inventory table --------------------------------------------------

/**
 * Table of every MX device in the selected org. Serial column drills into
 * the per-appliance detail scene; mac/lat/lng columns are hidden because
 * they're rarely useful in a dense inventory view.
 */
export function applianceInventoryTable(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.Devices,
    productTypes: ['appliance'],
  });
  return PanelBuilders.table()
    .setTitle('Appliance inventory')
    .setDescription('MX security appliances in the selected organization. Click a serial to drill in.')
    .setData(hideColumns(runner, ['lat', 'lng', 'mac']))
    .setNoValue('No appliance devices found in this organization.')
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideLinks([
        {
          title: 'Open appliance',
          url: `${PLUGIN_BASE_URL}/${ROUTES.Appliances}/\${__value.raw:percentencode}?var-org=\${var-org:queryparam}`,
        },
      ]);
      b.matchFieldsWithName('firmware').overrideCustomFieldConfig('width', 140);
      b.matchFieldsWithName('address').overrideCustomFieldConfig('width', 260);
    })
    .build();
}

// VPN peer matrix ------------------------------------------------------------

/**
 * VPN peer matrix — the primary VPN panel, backed by `ApplianceVpnStatuses`
 * (one row per peer-pair with reachability + identity + usage summary).
 *
 * Why not a merged (statuses × stats) frame: combining the two frames would
 * ideally produce one row per peer with both identity and per-uplink latency
 * /loss columns, but the join keys don't line up cleanly — the stats frame
 * keys rows on `(peerNetworkId, senderUplink, receiverUplink)` while the
 * statuses frame keys on `peerNetworkId` alone. A naive `joinByField` on
 * `peerNetworkId` would fan each peer into N rows (one per uplink pair)
 * which is worse than keeping the frames apart. Plan for v0.3.0: render the
 * statuses table as the primary panel and expose the stats frame on a
 * sibling panel ({@link vpnPeerStatsTable}); a follow-up can add a real
 * multi-key join once the Scenes transform registry grows one.
 *
 * The reachability column gets a green/red value mapping with a
 * color-background cell so operators can eyeball healthy tunnels at a glance.
 */
export function vpnPeerMatrixTable(): VizPanel {
  const statusesRunner = oneQuery({ kind: QueryKind.ApplianceVpnStatuses });

  return PanelBuilders.table()
    .setTitle('VPN peers')
    .setDescription(
      'Meraki AutoVPN + third-party peers for every network in the selected organization. Reachability is coloured; click a column header to sort.'
    )
    .setData(statusesRunner)
    .setNoValue('No VPN peers configured in the selected organization.')
    .setMappings([
      {
        type: 'value' as any,
        options: {
          reachable: { color: 'green', index: 0, text: 'Reachable' },
          unreachable: { color: 'red', index: 1, text: 'Unreachable' },
        },
      },
    ])
    .setOverrides((b) => {
      b.matchFieldsWithName('reachability').overrideCustomFieldConfig('cellOptions', {
        type: 'color-background',
      } as any);
      b.matchFieldsWithName('reachability').overrideCustomFieldConfig('width', 130);
      b.matchFieldsWithName('peerKind').overrideCustomFieldConfig('width', 100);
      b.matchFieldsWithName('vpnMode').overrideCustomFieldConfig('width', 100);
      b.matchFieldsWithName('sentKilobytes').overrideUnit('kbytes');
      b.matchFieldsWithName('receivedKilobytes').overrideUnit('kbytes');
    })
    .build();
}

/**
 * Companion table to {@link vpnPeerMatrixTable} — the aggregated VPN stats
 * frame (avg latency / jitter / loss / MOS + sent/received KB). Shown as a
 * sibling panel on the VPN tab because merging the stats and statuses frames
 * in a single panel requires a multi-runner SceneQueryRunner, which is more
 * plumbing than we want to own in v0.3.0. A follow-up can fold this into
 * {@link vpnPeerMatrixTable} once the join path above is wired up.
 */
export function vpnPeerStatsTable(): VizPanel {
  const runner = oneQuery({ kind: QueryKind.ApplianceVpnStats });
  return PanelBuilders.table()
    .setTitle('VPN peer stats')
    .setDescription(
      'Aggregated latency / jitter / loss / MOS per peer-pair over the selected time range.'
    )
    .setData(runner)
    .setNoValue('No VPN stats reported in the selected range.')
    .setOverrides((b) => {
      b.matchFieldsWithName('avgLatencyMs').overrideUnit('ms');
      b.matchFieldsWithName('avgJitter').overrideUnit('ms');
      b.matchFieldsWithName('avgLossPercentage').overrideUnit('percent');
      b.matchFieldsWithName('sentKilobytes').overrideUnit('kbytes');
      b.matchFieldsWithName('receivedKilobytes').overrideUnit('kbytes');
    })
    .build();
}

// Uplink loss / latency timeseries -------------------------------------------

/**
 * Native-timeseries panel for one appliance's uplink loss OR latency. The
 * backend emits one frame per (serial, uplink, ip, metric) — this factory
 * takes the metric discriminator as a parameter and filters the frame stream
 * by its `metric` label so loss and latency render on separate panels.
 *
 * Unit + bounds:
 *  - lossPercent → percent, min 0, max 100.
 *  - latencyMs → ms (no bounds; latency can spike into the seconds).
 *
 * `spanNulls: false` makes probe failures render as gaps rather than a
 * straight line through the missing window — important so operators can
 * eyeball "is this uplink actually down or just quiet".
 */
export function uplinkLossLatencyTimeseries(
  serial: string,
  metric: 'lossPercent' | 'latencyMs'
): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.DeviceUplinksLossLatency,
    serials: [serial],
    maxDataPoints: 500,
  });

  // The backend emits one frame per `(serial, uplink, ip, metric)` with
  // `metric` on the value field's labels and baked into the per-frame
  // `DisplayNameFromDS`. The Grafana `filterFieldsByName` regex matcher
  // matches the field's *display name* (via `getFieldDisplayName`), which
  // resolves to `DisplayNameFromDS` when it's set — so we keep the time
  // field (`ts`) AND value fields whose display name ends with the
  // selected metric. Frames whose value field doesn't match lose both
  // their fields and are dropped by the `filterFields` operator (see
  // @grafana/data's `filter.mjs`, `if (!fields.length) continue`).
  //
  // Pattern format: Grafana's `stringToJsRegex` expects `/pattern/flags`.
  // Alternation between the time field name and the metric suffix keeps
  // the time axis intact while pruning the wrong-metric frames.
  const filtered = new SceneDataTransformer({
    $data: runner,
    transformations: [
      {
        id: 'filterFieldsByName',
        options: {
          include: {
            // `ts` keeps the time field; `<metric>$` keeps the value
            // field whose baked DisplayNameFromDS ends with the metric.
            pattern: `/^ts$|${metric}$/`,
          },
        },
      },
    ],
  });

  const title = metric === 'lossPercent' ? 'Uplink loss' : 'Uplink latency';
  const description =
    metric === 'lossPercent'
      ? 'Per-uplink packet loss over time (5-minute probe samples).'
      : 'Per-uplink latency over time (5-minute probe samples).';

  const builder = PanelBuilders.timeseries()
    .setTitle(title)
    .setDescription(description)
    .setData(filtered)
    .setNoValue('No probe data in the selected range.')
    .setColor({ mode: FieldColorModeId.PaletteClassic })
    .setCustomFieldConfig('lineWidth', 2)
    .setCustomFieldConfig('fillOpacity', 10)
    // Probe failures show as nulls from the backend; rendering them as gaps
    // makes "down" obvious vs. "zero loss".
    .setCustomFieldConfig('spanNulls', false)
    .setCustomFieldConfig('showPoints', 'auto' as any)
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .setOption('tooltip', { mode: 'multi', sort: 'desc' } as any);

  if (metric === 'lossPercent') {
    builder.setUnit('percent').setMin(0).setMax(100);
  } else {
    builder.setUnit('ms');
  }

  return builder.build();
}

// Firewall tab panels --------------------------------------------------------

/**
 * Port forwarding rules for one network. The backend requires at least one
 * networkId — callers typically pass `$network` from the Firewall tab's
 * single-select network variable. Rendered as a plain table; no drilldown
 * because individual rules don't have a detail page.
 */
export function portForwardingTable(networkId: string): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.AppliancePortForwarding,
    networkIds: [networkId],
  });
  return PanelBuilders.table()
    .setTitle('Port forwarding rules')
    .setDescription('Inbound NAT rules configured on the selected network\'s MX.')
    .setData(hideColumns(runner, ['networkId']))
    .setNoValue('No port forwarding rules configured for this network.')
    .setOverrides((b) => {
      b.matchFieldsWithName('name').overrideCustomFieldConfig('width', 220);
      b.matchFieldsWithName('protocol').overrideCustomFieldConfig('width', 90);
      b.matchFieldsWithName('publicPort').overrideCustomFieldConfig('width', 110);
      b.matchFieldsWithName('localPort').overrideCustomFieldConfig('width', 110);
      b.matchFieldsWithName('lanIp').overrideCustomFieldConfig('width', 140);
      b.matchFieldsWithName('uplink').overrideCustomFieldConfig('width', 100);
    })
    .build();
}

/**
 * Appliance settings card for one network — tracking method, deployment
 * mode, dynamic DNS config. Rendered as a one-row table; a stack of stat
 * panels would be more compact but noisier on this tab.
 */
export function applianceSettingsCard(networkId: string): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.ApplianceSettings,
    networkIds: [networkId],
  });
  return PanelBuilders.table()
    .setTitle('Appliance settings')
    .setDescription('Client tracking, deployment mode, and dynamic DNS configuration.')
    .setData(hideColumns(runner, ['networkId']))
    .setNoValue('No appliance settings reported for this network.')
    .build();
}

// Per-appliance overview KPIs ------------------------------------------------

/**
 * KPI tiles for one MX — status, model, firmware, network. Derived from the
 * Devices table filtered to one serial plus the DeviceAvailabilities frame
 * for the live online/offline status. Each stat reads one column via a
 * filterFieldsByName transform so the stat viz picks up exactly one value —
 * mirrors `apOverviewKpiRow` and `switchOverviewKpiRow`.
 */
export function applianceOverviewKpiRow(serial: string): VizPanel[] {
  const deviceRunner = oneQuery({
    kind: QueryKind.Devices,
    serials: [serial],
    productTypes: ['appliance'],
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
    productTypes: ['appliance'],
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

  return [
    status,
    pickFromDevices('Model', 'model'),
    pickFromDevices('Firmware', 'firmware'),
    pickFromDevices('Network', 'networkId'),
  ];
}
