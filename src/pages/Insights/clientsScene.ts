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
  clientsOverviewKpiRow,
  topClientsTable,
  topDeviceModelsTable,
  topDevicesTable,
  topSsidsTable,
  topSwitchesByEnergyTable,
} from './panels';

/**
 * Clients tab — aggregate usage KPIs plus the family of top-N tables for
 * users / devices / SSIDs / models / energy. The backend `/summary/top/*`
 * endpoints don't accept a time range parameter (they use a bare
 * `timespan`), so this tab's dashboard time picker is more of a convention
 * than a filter; the top tables default to a 24-hour lookback on the
 * backend. A future follow-up can wire `timespanSeconds` from the picker
 * through `oneQuery`.
 *
 * Layout, top-down:
 *   1. Config-guard banner (hidden when the plugin is configured).
 *   2. KPI row: total clients + overall / downstream / upstream usage from
 *      `ClientsOverview`.
 *   3. Row of two tables: top clients + top devices. Both height 420 so
 *      they share a visual budget.
 *   4. Row of two tables: top SSIDs + top device models. Height 360.
 *   5. Full-width top switches by energy table. Height 360.
 */
export function clientsScene(): EmbeddedScene {
  const kpiItems = clientsOverviewKpiRow().map(
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
        // Row 1: top clients + top devices (same visual weight so they
        // share a row split evenly).
        new SceneFlexItem({
          height: 420,
          body: new SceneFlexLayout({
            direction: 'row',
            children: [
              new SceneFlexItem({ body: topClientsTable() }),
              new SceneFlexItem({ body: topDevicesTable() }),
            ],
          }),
        }),
        // Row 2: top SSIDs + top device models.
        new SceneFlexItem({
          height: 360,
          body: new SceneFlexLayout({
            direction: 'row',
            children: [
              new SceneFlexItem({ body: topSsidsTable() }),
              new SceneFlexItem({ body: topDeviceModelsTable() }),
            ],
          }),
        }),
        // Row 3: full-width top switches by energy.
        new SceneFlexItem({
          height: 360,
          body: topSwitchesByEnergyTable(),
        }),
      ],
    }),
  });
}
