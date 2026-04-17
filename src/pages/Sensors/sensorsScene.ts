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
import {
  networkVariable,
  orgVariable,
} from '../../scene-helpers/variables';
import {
  ALL_SENSOR_METRICS,
  sensorInventoryTable,
  sensorKpiRow,
  sensorMetricCard,
} from '../../scene-helpers/panels';

/**
 * Sensors overview — a dense dashboard-style view with a KPI row, one
 * small timeseries panel per metric type (overlaying all sensors) and a
 * clickable inventory table. Clicking a row opens the sensor detail page
 * via the drilldown wired on `sensorsPage`.
 */
export function sensorsScene() {
  const cards = ALL_SENSOR_METRICS.map(
    (meta) => new SceneCSSGridItem({ body: sensorMetricCard(meta) })
  );

  const kpis = sensorKpiRow().map(
    (panel) => new SceneCSSGridItem({ body: panel })
  );

  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [orgVariable(), networkVariable()],
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
            templateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
            autoRows: '100px',
            rowGap: 1,
            columnGap: 1,
            children: kpis,
          }),
        }),
        new SceneFlexItem({
          body: new SceneCSSGridLayout({
            templateColumns: 'repeat(auto-fit, minmax(360px, 1fr))',
            autoRows: '260px',
            rowGap: 1,
            columnGap: 1,
            children: cards,
          }),
        }),
        new SceneFlexItem({
          minHeight: 420,
          body: sensorInventoryTable(),
        }),
      ],
    }),
  });
}
