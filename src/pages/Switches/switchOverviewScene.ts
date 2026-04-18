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
import { switchOverviewKpiRow } from './panels';

/**
 * Overview tab for a single switch — four KPI tiles (status, model,
 * firmware, client count). The deeper per-port data lives on the Ports tab,
 * keeping this page fast to render and quick to eyeball.
 */
export function switchOverviewScene(serial: string): EmbeddedScene {
  const kpiItems = switchOverviewKpiRow(serial).map(
    (panel) => new SceneCSSGridItem({ body: panel })
  );

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
      ],
    }),
  });
}
