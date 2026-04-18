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
import { alertsKpiRow, alertsTable } from '../Alerts/panels';
import { severityVariable } from '../Alerts/variables';

/**
 * Alerts tab for a single organization. Shows the same KPI row and
 * alerts table as the top-level Alerts page, but scoped to the orgId
 * supplied by the drilldown parent.
 *
 * Why pass `orgId` directly (rather than relying on a `$org` variable):
 * the Organization detail page does not own a `$org` variable — the org
 * context comes from the URL parameter. Using the literal ID keeps this
 * tab working even when the user deep-links without a variable selector.
 *
 * A local `$severity` variable is included so the panels in
 * `src/pages/Alerts/panels.ts` resolve their `metrics: ['$severity']`
 * filter cleanly (see that file's header comment for the severity-via-
 * metrics contract). Defaulting to "All" means the tab is populated out
 * of the gate; users can narrow via the picker at the top.
 *
 * Time range defaults to 24h to match the top-level Alerts page.
 */
export function organizationAlertsScene(orgId: string): EmbeddedScene {
  const kpiItems = alertsKpiRow(orgId).map(
    (panel) => new SceneCSSGridItem({ body: panel })
  );

  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-24h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [severityVariable()],
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
          minHeight: 420,
          body: alertsTable(orgId),
        }),
      ],
    }),
  });
}
