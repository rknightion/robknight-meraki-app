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
  networkVariableForProductTypes,
  orgVariable,
} from '../../scene-helpers/variables';
import { configGuardFlexItem } from '../../scene-helpers/ConfigGuard';
import {
  mgInventoryTable,
  mgSignalBarChart,
  mgStatusKpiRow,
  mgUplinkFleetTable,
} from './panels';
import { mgVariable } from './variables';

/**
 * Cellular Gateways overview — a dashboard-style view mirroring the
 * AccessPoints / Cameras scenes:
 *   1. Config-not-set banner (collapses to zero height when configured).
 *   2. Three KPI tiles: online / alerting / offline counts for MG devices.
 *   3. Fleet-wide uplink status table with per-row signal metrics.
 *   4. RSRP bar chart for at-a-glance signal-strength comparison.
 *   5. Gateway inventory with drilldown.
 */
export function cellularGatewaysScene(): EmbeddedScene {
  const kpis = mgStatusKpiRow().map((panel) => new SceneCSSGridItem({ body: panel }));

  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [
        orgVariable(),
        networkVariableForProductTypes(['cellularGateway']),
        mgVariable(),
      ],
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
          minHeight: 360,
          body: mgUplinkFleetTable(),
        }),
        new SceneFlexItem({
          height: 280,
          body: mgSignalBarChart(),
        }),
        new SceneFlexItem({
          minHeight: 420,
          body: mgInventoryTable(),
        }),
      ],
    }),
  });
}
