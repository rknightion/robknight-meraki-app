import {
  PanelBuilders,
  SceneDataTransformer,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';

// Shared query-runner factory. Mirrors the per-area `oneQuery` helper
// elsewhere in the codebase (Insights, AccessPoints, Switches) — kept local
// so the Traffic area doesn't depend on internal helpers in the shared
// panels module.

interface QueryParams {
  refId?: string;
  kind: QueryKind;
  orgId?: string;
  networkIds?: string[];
  metrics?: string[];
  timespanSeconds?: number;
  maxDataPoints?: number;
}

function oneQuery(params: QueryParams): SceneQueryRunner {
  const {
    refId = 'A',
    kind,
    orgId,
    networkIds,
    metrics,
    timespanSeconds,
    maxDataPoints,
  } = params;

  const query: Record<string, unknown> & { refId: string } = { refId, kind };
  query.orgId = orgId ?? '$org';
  if (networkIds && networkIds.length > 0) {
    query.networkIds = networkIds;
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

/**
 * Top applications by usage table. Drilldown-free — the org-level summary
 * doesn't carry a per-app detail page; Meraki's dashboard surfaces app
 * detail through the per-network traffic view, which the per-network table
 * below already covers.
 *
 * Quantity is fixed at 25 (sent via the `metrics` overload — see
 * `parseQuantity` in `pkg/plugin/query/traffic.go`). Hard-coded rather than
 * variable-bound because the table panel itself caps visible rows; a higher
 * server-side limit just costs us more bandwidth without changing what the
 * user sees.
 */
export function topApplicationsTable(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.TopApplicationsByUsage,
    metrics: ['25'],
  });

  return PanelBuilders.table()
    .setTitle('Top applications by usage')
    .setDescription(
      'Top L7 applications across the organisation, sorted by total usage. ' +
        'Meraki caps the lookback at 186 days and requires at least ~12h of ' +
        'data for the summary — shorter windows render empty.'
    )
    .setData(runner)
    .setNoValue('No application data available for the selected window.')
    .setOverrides((b) => {
      b.matchFieldsWithName('totalMb').overrideUnit('decmbytes');
      b.matchFieldsWithName('downstreamMb').overrideUnit('decmbytes');
      b.matchFieldsWithName('upstreamMb').overrideUnit('decmbytes');
      b.matchFieldsWithName('percentage').overrideUnit('percent');
      b.matchFieldsWithName('name').overrideCustomFieldConfig('width', 200);
    })
    .build();
}

/**
 * Top applications by usage as a horizontal bar chart. Reads the same wide
 * frame as `topApplicationsTable` but uses the `percentage` column as the
 * value axis to give an at-a-glance distribution. Sits next to the table on
 * the page so users can pick the format that suits their workflow.
 */
export function topApplicationsBarChart(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.TopApplicationsByUsage,
    metrics: ['25'],
  });

  // The bar chart picks the first non-name numeric column by default — keep
  // only `name` and `totalMb` so the rendering is deterministic regardless
  // of upstream column ordering.
  const data = hideColumns(runner, [
    'category',
    'downstreamMb',
    'upstreamMb',
    'percentage',
    'clientCount',
  ]);

  return PanelBuilders.barchart()
    .setTitle('Top applications (volume)')
    .setDescription('Total bytes sent + received per application across the org.')
    .setData(data)
    .setNoValue('No application data available for the selected window.')
    .setUnit('decmbytes')
    .setOption('orientation', 'horizontal' as any)
    .setOption('legend', { showLegend: false } as any)
    .build();
}

/**
 * Top application categories table. Same shape as `topApplicationsTable`
 * minus the `category` column (each row IS a category here).
 */
export function topApplicationCategoriesTable(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.TopApplicationCategoriesByUsage,
    metrics: ['25'],
  });

  return PanelBuilders.table()
    .setTitle('Top application categories')
    .setDescription(
      'Top L7 application categories across the organisation. ' +
        'Categories aggregate related applications (Video, Audio, Software updates, …).'
    )
    .setData(runner)
    .setNoValue('No category data available for the selected window.')
    .setOverrides((b) => {
      b.matchFieldsWithName('totalMb').overrideUnit('decmbytes');
      b.matchFieldsWithName('downstreamMb').overrideUnit('decmbytes');
      b.matchFieldsWithName('upstreamMb').overrideUnit('decmbytes');
      b.matchFieldsWithName('percentage').overrideUnit('percent');
      b.matchFieldsWithName('name').overrideCustomFieldConfig('width', 220);
    })
    .build();
}

/**
 * Per-network traffic mix. Renders the (network × application × destination)
 * row table from /networks/{id}/traffic for every selected network. The
 * panel uses the dashboard time range (clamped to 30d by the backend) and
 * the `$deviceType` variable for filtering.
 *
 * The `port` column is rendered as `0` when the underlying Meraki row had
 * no port (some application rows don't carry a destination port). Hidden
 * here to keep the table tidy; users can un-hide via the field config if
 * they need it.
 */
export function networkTrafficTable(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.NetworkTraffic,
    networkIds: ['$network'],
    metrics: ['$deviceType'],
  });

  // `category` comes through empty (the per-network endpoint doesn't carry
  // a category column) — hide it from the default view.
  const data = hideColumns(runner, ['category']);

  return PanelBuilders.table()
    .setTitle('Per-network L7 traffic')
    .setDescription(
      'L7 application breakdown for each selected network. Requires traffic ' +
        'analysis (basic or detailed) to be enabled on the network — the ' +
        'TrafficGuard banner above flags networks where the panel will be empty.'
    )
    .setData(data)
    .setNoValue('No traffic data — check that traffic analysis is enabled on the selected networks.')
    .setOverrides((b) => {
      b.matchFieldsWithName('sentMb').overrideUnit('decmbytes');
      b.matchFieldsWithName('recvMb').overrideUnit('decmbytes');
      b.matchFieldsWithName('totalMb').overrideUnit('decmbytes');
      b.matchFieldsWithName('activeTime').overrideUnit('s');
      b.matchFieldsWithName('networkId').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('application').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('destination').overrideCustomFieldConfig('width', 240);
    })
    .build();
}
