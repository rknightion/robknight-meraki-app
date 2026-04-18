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
  sensorAqiCompositeTile,
  sensorBatteryTimeline,
  sensorFloorPlanHeatmap,
  sensorInventoryTable,
  sensorKpiRow,
  sensorMetricCard,
} from '../../scene-helpers/panels';
import { configGuardFlexItem } from '../../scene-helpers/ConfigGuard';

/**
 * Sensors overview — a dense dashboard-style view with a KPI row, one
 * small timeseries panel per metric type (overlaying all sensors) and a
 * clickable inventory table. Clicking a row opens the sensor detail page
 * via the drilldown wired on `sensorsPage`.
 *
 * v0.5 §4.4.3-1e adds three panels to this scene (floor-plan heatmap,
 * AQI composite, battery timeline). The roadmap offered a "Spaces" tab
 * as an alternative layout; we opted to stack the new panels in-line
 * with the existing metric cards because (a) the data is closely related
 * (same `$org`/$network` scope, same MT family), (b) a tab would force
 * a second drilldown + URL-sync boundary that operators would have to
 * remember, and (c) Grafana users skim top-to-bottom — adding a row of
 * composite tiles is the pattern the other overview pages already use.
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
        configGuardFlexItem(),
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
        // v0.5 §4.4.3-1e composite tiles. Stacked as a two-up row: the
        // AQI tile on the left, battery timeline on the right. Below
        // them the full-width floor-plan heatmap (or grid fallback).
        new SceneFlexItem({
          height: 260,
          body: new SceneCSSGridLayout({
            templateColumns: 'repeat(auto-fit, minmax(360px, 1fr))',
            autoRows: '260px',
            rowGap: 1,
            columnGap: 1,
            children: [
              new SceneCSSGridItem({ body: sensorAqiCompositeTile() }),
              new SceneCSSGridItem({ body: sensorBatteryTimeline() }),
            ],
          }),
        }),
        new SceneFlexItem({
          minHeight: 320,
          body: sensorFloorPlanHeatmap(),
        }),
        new SceneFlexItem({
          minHeight: 420,
          body: sensorInventoryTable(),
        }),
      ],
    }),
  });
}
