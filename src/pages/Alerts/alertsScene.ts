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
  alertsKpiRow,
  alertsTable,
  alertsTimelineBarChart,
} from './panels';
import { severityVariable } from './variables';

/**
 * Alerts overview scene — the top-level "what's firing right now?" view.
 *
 * Layout, top-down:
 *   1. Config-guard banner (hidden when the plugin is configured).
 *   2. KPI row: critical / warning / informational counts from
 *      `AlertsOverview`.
 *   3. Timeline bar chart: alert volume stacked by severity over time.
 *   4. Full alerts table: every alert in the selected window, filtered
 *      by severity / org.
 *
 * Default time range is 24h — alerts are a diagnostics surface, so a
 * longer default window than the sensor scenes (6h) is deliberate.
 */
export function alertsScene(): EmbeddedScene {
  const kpiItems = alertsKpiRow().map(
    (panel) => new SceneCSSGridItem({ body: panel })
  );

  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-24h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [orgVariable(), severityVariable()],
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
            templateColumns: 'repeat(auto-fit, minmax(220px, 1fr))',
            autoRows: '100px',
            rowGap: 1,
            columnGap: 1,
            children: kpiItems,
          }),
        }),
        new SceneFlexItem({
          height: 260,
          body: alertsTimelineBarChart(),
        }),
        new SceneFlexItem({
          minHeight: 420,
          body: alertsTable(),
        }),
      ],
    }),
  });
}
