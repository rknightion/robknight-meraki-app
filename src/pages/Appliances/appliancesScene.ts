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
  applianceInventoryTable,
  applianceUplinkStatusTable,
  applianceUplinksOverviewRow,
  mxStatusKpiRow,
} from './panels';
import { mxVariable } from './variables';

/**
 * Appliances overview — a dashboard-style view that mirrors the Access
 * Points / Switches scaffolds:
 *   1. Config-not-set banner (collapses when configured).
 *   2. MX status KPI row (online / alerting / offline).
 *   3. Uplink overview KPI row (active / ready / failed / not connected).
 *   4. Uplink status table with per-serial drilldown.
 *   5. Appliance inventory table.
 *
 * `$mx` is included as a convenience filter for the per-appliance panels;
 * the inventory and overview tables ignore it so users can keep the filter
 * state while still seeing the whole fleet.
 */
export function appliancesScene(): EmbeddedScene {
  const statusKpis = mxStatusKpiRow().map(
    (panel) => new SceneCSSGridItem({ body: panel })
  );
  const uplinkKpis = applianceUplinksOverviewRow().map(
    (panel) => new SceneCSSGridItem({ body: panel })
  );

  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [
        orgVariable(),
        networkVariableForProductTypes(['appliance']),
        mxVariable(),
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
            children: statusKpis,
          }),
        }),
        new SceneFlexItem({
          height: 120,
          body: new SceneCSSGridLayout({
            templateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
            autoRows: '100px',
            rowGap: 1,
            columnGap: 1,
            children: uplinkKpis,
          }),
        }),
        new SceneFlexItem({
          minHeight: 420,
          body: applianceUplinkStatusTable(),
        }),
        new SceneFlexItem({
          minHeight: 420,
          body: applianceInventoryTable(),
        }),
      ],
    }),
  });
}
