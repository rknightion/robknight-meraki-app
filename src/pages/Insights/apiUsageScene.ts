import {
  EmbeddedScene,
  SceneCSSGridItem,
  SceneCSSGridLayout,
  SceneControlsSpacer,
  SceneFlexItem,
  SceneFlexLayout,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
  SceneVariableSet,
  VariableValueSelectors,
} from '@grafana/scenes';
import { orgVariable } from '../../scene-helpers/variables';
import { configGuardFlexItem } from '../../scene-helpers/ConfigGuard';
import {
  apiRequestsByIntervalChart,
  apiRequestRateWith429Overlay,
  apiRequestsKpiRow,
} from './panels';

/**
 * API Usage tab — visibility into the Meraki Dashboard API consumption for
 * the selected organization. Useful for spotting rate-limit approach (the
 * 429 tile + bar colour stand out) and for correlating dashboard-side
 * slowness with API behaviour.
 *
 * Default range is 24h. Meraki's apiRequests/overview endpoints cap at 31d
 * on their side; the backend clamps if the user picks a longer window.
 *
 * Layout, top-down:
 *   1. Config-guard banner (hidden when the plugin is configured).
 *   2. KPI row: total / 2xx / 4xx / 429 / 5xx counters from
 *      `ApiRequestsOverview`.
 *   3. Stacked bar chart of API requests by interval, coloured per HTTP
 *      class. The backend emits one frame per class with a baked
 *      DisplayNameFromDS, so the chart legends correctly out of the box.
 */
export function apiUsageScene(): EmbeddedScene {
  const kpiItems = apiRequestsKpiRow().map(
    (panel) => new SceneCSSGridItem({ body: panel })
  );

  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-24h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [orgVariable()],
    }),
    controls: [
      new VariableValueSelectors({}),
      new SceneControlsSpacer(),
      new SceneTimePicker({ isOnCanvas: true }),
      new SceneRefreshPicker({ intervals: ['30s', '1m', '5m'], isOnCanvas: true }),
    ],
    body: new SceneFlexLayout({
      direction: 'column',
      children: [
        configGuardFlexItem(),
        new SceneFlexItem({
          height: 120,
          body: new SceneCSSGridLayout({
            templateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
            autoRows: '100px',
            rowGap: 1,
            columnGap: 1,
            children: kpiItems,
          }),
        }),
        new SceneFlexItem({
          height: 380,
          body: apiRequestsByIntervalChart(),
        }),
        // §4.4.3-1f — request-rate timeseries with 429 overlay.
        new SceneFlexItem({
          height: 380,
          body: apiRequestRateWith429Overlay(),
        }),
      ],
    }),
  });
}
