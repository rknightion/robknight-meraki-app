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
  clientsPerSwitchBarChart,
  fleetPoeHistoryTimeseries,
  switchInventoryTable,
  switchKpiRow,
  switchPortsBySpeedStatPanel,
  switchPortsUsageHistoryTimeseries,
} from './panels';

/**
 * Switches overview — KPI row across the org (total switches, ports, PoE,
 * alerting) plus a clickable inventory table. Clicking a serial opens the
 * per-switch detail page via the drilldown wired on `switchesPage`.
 *
 * Only the org variable is exposed at this level; network scoping adds
 * value on the per-switch Ports tab but muddies the fleet-wide KPIs.
 */
export function switchesScene() {
  const kpis = switchKpiRow().map((panel) => new SceneCSSGridItem({ body: panel }));

  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
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
            templateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
            autoRows: '100px',
            rowGap: 1,
            columnGap: 1,
            children: kpis,
          }),
        }),
        // §3.1 — Ports by speed + usage history.
        new SceneFlexLayout({
          direction: 'row',
          children: [
            new SceneFlexItem({
              width: '30%',
              minHeight: 280,
              body: switchPortsBySpeedStatPanel(),
            }),
            new SceneFlexItem({
              width: '70%',
              minHeight: 280,
              body: switchPortsUsageHistoryTimeseries(),
            }),
          ],
        }),
        // v0.8 — fleet PoE history + clients-per-switch. Left half is a
        // 7d-ish timeseries (Meraki auto-buckets); right half is a
        // one-row-per-switch "who's carrying clients" table.
        new SceneFlexLayout({
          direction: 'row',
          children: [
            new SceneFlexItem({
              width: '60%',
              minHeight: 280,
              body: fleetPoeHistoryTimeseries(),
            }),
            new SceneFlexItem({
              width: '40%',
              minHeight: 280,
              body: clientsPerSwitchBarChart(),
            }),
          ],
        }),
        new SceneFlexItem({
          minHeight: 520,
          body: switchInventoryTable(),
        }),
      ],
    }),
  });
}
