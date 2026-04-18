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
  cameraInventoryTable,
  cameraOnboardingTable,
  cameraStatusKpiRow,
} from './panels';
import { cameraVariable } from './variables';

/**
 * Cameras overview — a dashboard-style view that mirrors the Access Points
 * and Switches scenes:
 *   1. Config-not-set banner (collapses to zero height when configured).
 *   2. Three KPI tiles: online / alerting / offline counts for MV devices.
 *   3. Onboarding status table (per-row drilldown via backend drilldownUrl).
 *   4. Camera inventory table with serial-level drilldown.
 *
 * `$camera` is exposed on the variable bar for symmetry with the AP/MS
 * scenes even though no panel binds to it directly here — the user may pin
 * the variable and drill in without losing filter state.
 */
export function camerasScene(): EmbeddedScene {
  const kpis = cameraStatusKpiRow().map((panel) => new SceneCSSGridItem({ body: panel }));

  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [
        orgVariable(),
        networkVariableForProductTypes(['camera']),
        cameraVariable(),
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
          body: cameraOnboardingTable(),
        }),
        new SceneFlexItem({
          minHeight: 420,
          body: cameraInventoryTable(),
        }),
      ],
    }),
  });
}
