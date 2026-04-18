import {
  PanelBuilders,
  SceneDataTransformer,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';
import { urlForClient } from './links';

// Shared query-runner factory ------------------------------------------------
//
// Local copy of the `oneQuery` shape used by every other per-area panels file.
// We thread $org/$network/$client through the panel queries via Grafana
// template variables — the datasource's `applyTemplateVariables` expands them
// into a real array before POSTing.

interface QueryParams {
  refId?: string;
  kind: QueryKind;
  orgId?: string;
  networkIds?: string[];
  metrics?: string[];
  maxDataPoints?: number;
}

function oneQuery(params: QueryParams): SceneQueryRunner {
  const { refId = 'A', kind, orgId, networkIds, metrics, maxDataPoints } = params;
  const query: Record<string, unknown> & { refId: string } = { refId, kind };
  if (orgId !== undefined) {
    query.orgId = orgId;
  } else {
    query.orgId = '$org';
  }
  if (networkIds && networkIds.length > 0) {
    query.networkIds = networkIds;
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

// Top-talkers tab ------------------------------------------------------------

/**
 * `topTalkersTable` — full-width table of clients ranked by total usage,
 * driven by the existing `topClients` org-wide summary kind. We add a
 * drilldown link on the `mac` column so operators can click through to the
 * per-client detail page.
 *
 * Column hides: `id` is opaque to humans, `drilldownUrl` is internal — the
 * frame doesn't carry the latter, but we keep the hide clause future-proof
 * if the backend starts emitting one.
 */
export function topTalkersTable(): VizPanel {
  const runner = oneQuery({ kind: QueryKind.TopClients });
  const data = hideColumns(runner, ['id']);

  return PanelBuilders.table()
    .setTitle('Top talkers')
    .setDescription('Clients with the highest total usage in the last 24 hours.')
    .setData(data)
    .setNoValue('No client activity in the selected window.')
    .setOverrides((b) => {
      b.matchFieldsWithName('mac').overrideLinks([
        {
          title: 'Open client',
          // urlForClient is built statically here; the scene-time
          // ${__data.fields.mac} substitution makes the URL dynamic per row.
          url: urlForClient('${__data.fields.mac}', '${__url_time_range}'),
        },
      ]);
      b.matchFieldsWithName('usageTotal').overrideUnit('mbytes');
      b.matchFieldsWithName('usageSent').overrideUnit('mbytes');
      b.matchFieldsWithName('usageRecv').overrideUnit('mbytes');
      b.matchFieldsWithName('mac').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('name').overrideCustomFieldConfig('width', 200);
    })
    .build();
}

// New-clients tab ------------------------------------------------------------

/**
 * `newClientsTable` — clients first seen in the last 24h on any of the
 * selected networks. Backend `clientsList` returns one row per client per
 * network within the panel time range; the frontend filters down to rows
 * whose `firstSeen` falls in the lookback window via a `filterByValue`
 * transform. We drop the `lastSeen` & `usage*` columns to keep the new-
 * clients view focused on identity rather than traffic.
 *
 * The "first 24h" window is enforced by the dashboard time picker (default
 * 24h on the parent scene). When operators broaden the window the table
 * widens accordingly — same UX as the Audit Log table.
 */
export function newClientsTable(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.ClientsList,
    networkIds: ['$network'],
  });
  const data = hideColumns(runner, [
    'usageSentKb',
    'usageRecvKb',
    'lastSeen',
    'recentDeviceSerial',
  ]);

  return PanelBuilders.table()
    .setTitle('New clients')
    .setDescription('Clients first observed within the dashboard time window. Defaults to 24h.')
    .setData(data)
    .setNoValue('No new clients in the selected window.')
    .setOverrides((b) => {
      b.matchFieldsWithName('mac').overrideLinks([
        {
          title: 'Open client',
          url: urlForClient('${__data.fields.mac}', '${org}'),
        },
      ]);
      b.matchFieldsWithName('mac').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('hostname').overrideCustomFieldConfig('width', 220);
      b.matchFieldsWithName('vlan').overrideCustomFieldConfig('width', 80);
      b.matchFieldsWithName('ssid').overrideCustomFieldConfig('width', 160);
      b.matchFieldsWithName('firstSeen').overrideCustomFieldConfig('width', 200);
      b.matchFieldsWithName('usageTotalKb').overrideUnit('kbytes');
    })
    .setOption('sortBy', [{ displayName: 'firstSeen', desc: true }] as any)
    .build();
}

// Search tab -----------------------------------------------------------------

/**
 * `clientSearchTable` — single-MAC lookup table backed by the `clientLookup`
 * kind. Empty MAC and not-found both surface as a zero-row frame with an
 * Info notice (the §G.20 pattern) — Grafana renders the notice inline above
 * the empty grid.
 *
 * The MAC value comes from the `$client` variable; the panel re-runs as
 * soon as the user types into the picker.
 */
export function clientSearchTable(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.ClientLookup,
    metrics: ['$client'],
  });

  return PanelBuilders.table()
    .setTitle('Client search')
    .setDescription(
      'Look up a client across every network in the organization by MAC. ' +
        'Empty searches and unknown MACs render an empty result with a hint.'
    )
    .setData(runner)
    .setNoValue('Enter a MAC address (or partial MAC) above.')
    .setOverrides((b) => {
      b.matchFieldsWithName('mac').overrideLinks([
        {
          title: 'Open client',
          url: urlForClient('${__data.fields.mac}', '${org}'),
        },
      ]);
      b.matchFieldsWithName('usageTotalKb').overrideUnit('kbytes');
      b.matchFieldsWithName('usageSentKb').overrideUnit('kbytes');
      b.matchFieldsWithName('usageRecvKb').overrideUnit('kbytes');
      b.matchFieldsWithName('mac').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('networkName').overrideCustomFieldConfig('width', 200);
      b.matchFieldsWithName('user').overrideCustomFieldConfig('width', 200);
    })
    .build();
}

// Session-history tab + per-client drilldown --------------------------------

/**
 * `sessionHistoryTimeseries` — per-client wireless latency history. The
 * backend emits one frame per traffic category (overall / background /
 * bestEffort / video / voice) with a labelled value field, so the panel
 * picks up the legend natively without a client-side pivot.
 *
 * Used both on the parent Clients page (Session history tab — surfaces the
 * latest selected client) and the per-client drilldown.
 */
export function sessionHistoryTimeseries(): VizPanel {
  return PanelBuilders.timeseries()
    .setTitle('Session latency history')
    .setDescription(
      'Per-client wireless latency, broken down by traffic category. ' +
        'Requires the client to be on a wireless network — wired-only ' +
        'clients render an empty chart.'
    )
    .setData(
      oneQuery({
        kind: QueryKind.ClientSessions,
        networkIds: ['$network'],
        metrics: ['$client'],
        maxDataPoints: 200,
      })
    )
    .setNoValue('Pick a client and a wireless network to see latency history.')
    .setUnit('ms')
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .build();
}

/**
 * `clientLatencyTrend` — same data as `sessionHistoryTimeseries` but rendered
 * as a single-stat sparkline so the per-client drilldown header can show a
 * compact "current latency" tile next to the full timeseries.
 *
 * Reads from the same `clientSessions` query so the runtime cost is shared
 * with the timeseries panel via the dispatcher's per-key request coalescing
 * (singleflight).
 */
export function clientLatencyTrend(): VizPanel {
  return PanelBuilders.stat()
    .setTitle('Recent latency')
    .setDescription('Average latency in the most recent bucket of the selected window.')
    .setData(
      oneQuery({
        kind: QueryKind.ClientSessions,
        networkIds: ['$network'],
        metrics: ['$client'],
        maxDataPoints: 100,
      })
    )
    .setNoValue('—')
    .setUnit('ms')
    .setOption('reduceOptions', {
      values: false,
      calcs: ['lastNotNull'],
      fields: '',
    } as any)
    .setOption('graphMode', 'area' as any)
    .build();
}
