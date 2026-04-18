import { FieldColorModeId } from '@grafana/schema';
import {
  PanelBuilders,
  SceneDataTransformer,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';

// Shared query-runner factory -------------------------------------------------

interface NetworkEventsQueryOpts {
  refId?: string;
  maxDataPoints?: number;
}

/**
 * Build a `SceneQueryRunner` for the NetworkEvents kind. The variables are
 * baked in so both panels in this file share exactly the same query shape:
 *   - networkIds: `['$network']`
 *   - productTypes: `['$productType']`
 *   - metrics: `['$eventType']` (backend forwards as includedEventTypes[])
 *
 * Kept local to the Events area so the variable contract lives next to the
 * panels that consume it.
 */
function networkEventsQuery(opts: NetworkEventsQueryOpts = {}): SceneQueryRunner {
  const { refId = 'A', maxDataPoints } = opts;
  return new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [
      {
        refId,
        kind: QueryKind.NetworkEvents,
        orgId: '$org',
        networkIds: ['$network'],
        productTypes: ['$productType'],
        metrics: ['$eventType'],
      },
    ],
    ...(maxDataPoints ? { maxDataPoints } : {}),
  });
}

// Events feed ---------------------------------------------------------------

/**
 * Full events table — one row per event in the selected window. Columns
 * match the backend frame shape in
 * `pkg/plugin/query/events.go::handleNetworkEvents`:
 *   occurredAt, productType, category, type, description, device_serial,
 *   device_name, client_id, client_mac, client_description, network_id,
 *   drilldownUrl.
 *
 * The `device_serial` column carries a drilldown link to the per-device
 * detail page derived from the row's own productType — the backend emits
 * `drilldownUrl` per row so the link routes to the right family without
 * any frontend template branching.
 */
export function eventsTable(): VizPanel {
  const runner = networkEventsQuery();
  const organized = new SceneDataTransformer({
    $data: runner,
    transformations: [
      {
        id: 'organize',
        options: {
          excludeByName: {
            // Hide lower-signal columns by default. The drilldownUrl column
            // is consumed by the device_serial link below, not surfaced to
            // the operator as a separate cell.
            client_id: true,
            client_description: true,
            drilldownUrl: true,
          },
          renameByName: {},
        },
      },
    ],
  });

  return PanelBuilders.table()
    .setTitle('Events')
    .setDescription('Network events in the selected window — filtered by product type and event type.')
    .setData(organized)
    .setNoValue('No events in the selected range.')
    .setOverrides((b) => {
      b.matchFieldsWithName('device_serial').overrideLinks([
        {
          // Per-row drilldown via the backend-emitted URL — cross-family
          // safe. Rows without a device populate an empty string, which
          // Grafana treats as no link.
          title: 'Open device',
          url: '${__data.fields.drilldownUrl}',
        },
      ]);
      b.matchFieldsWithName('occurredAt').overrideCustomFieldConfig('width', 190);
      b.matchFieldsWithName('productType').overrideCustomFieldConfig('width', 130);
      b.matchFieldsWithName('category').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('type').overrideCustomFieldConfig('width', 220);
      b.matchFieldsWithName('device_serial').overrideCustomFieldConfig('width', 160);
      b.matchFieldsWithName('device_name').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('client_mac').overrideCustomFieldConfig('width', 160);
    })
    .build();
}

/**
 * Events timeline — a stacked bar chart bucketed by time. Backed by the
 * server-side `NetworkEventsTimeline` aggregator which emits a wide frame
 * `{ts, <category1>, <category2>, ...}` with zero-filled buckets. Previous
 * versions used a client-side `groupingToMatrix` transform that emitted
 * string cells and tripped the barchart viz with "No numeric fields found".
 */
export function eventsTimelineBarChart(): VizPanel {
  const runner = new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [
      {
        refId: 'A',
        kind: QueryKind.NetworkEventsTimeline,
        orgId: '$org',
        networkIds: ['$network'],
        productTypes: ['$productType'],
        metrics: ['$eventType'],
      },
    ],
  });

  return PanelBuilders.barchart()
    .setTitle('Event timeline')
    .setDescription('Event volume over the selected window, stacked by category.')
    .setData(runner)
    .setNoValue('No events in the selected range.')
    .setOption('stacking', 'normal' as any)
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .setColor({ mode: FieldColorModeId.PaletteClassic })
    .setCustomFieldConfig('fillOpacity', 80)
    .setCustomFieldConfig('lineWidth', 0)
    .build();
}
