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
} from '@grafana/scenes';
import { orgOnlyVariables } from '../../scene-helpers/variables';
import {
  switchOverviewKpiRow,
  switchPoeBudgetStat,
  switchVlanDistributionDonut,
} from './panels';

/**
 * Overview tab for a single switch — KPI tiles (status, model, firmware,
 * client count) plus §4.4.3-1b additions: a PoE draw stat and a VLAN
 * distribution donut. The deeper per-port data lives on the Ports tab,
 * keeping this page fast to render and quick to eyeball.
 */
export function switchOverviewScene(serial: string): EmbeddedScene {
  const kpiItems = switchOverviewKpiRow(serial).map(
    (panel) => new SceneCSSGridItem({ body: panel })
  );
  // PoE draw sits beside the existing four KPIs — it's a scalar summary of
  // the ports feed we already fetch. Five tiles total at 200px minmax width.
  kpiItems.push(new SceneCSSGridItem({ body: switchPoeBudgetStat(serial) }));

  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
    // Drilldowns inherit their `$org` value via the var-org query param,
    // but the scene must *declare* the variable for the URL value to
    // hydrate — without this the panels ship `orgId: '$org'` literally and
    // Meraki requests short-circuit with "orgId is required".
    $variables: orgOnlyVariables(),
    controls: [
      new SceneControlsSpacer(),
      new SceneTimePicker({ isOnCanvas: true }),
      new SceneRefreshPicker({ intervals: ['30s', '1m', '5m'], isOnCanvas: true }),
    ],
    body: new SceneFlexLayout({
      direction: 'column',
      children: [
        new SceneFlexItem({
          height: 160,
          body: new SceneCSSGridLayout({
            templateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
            autoRows: '140px',
            rowGap: 1,
            columnGap: 1,
            children: kpiItems,
          }),
        }),
        // VLAN distribution donut below the KPI row. Scoped to this switch
        // via the serial — the org-wide default aggregates every switch in
        // the estate, which isn't what an operator on the detail page wants.
        new SceneFlexItem({
          minHeight: 320,
          body: switchVlanDistributionDonut(serial),
        }),
      ],
    }),
  });
}
