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
import { cameraOnboardingTable, cameraOverviewKpiRow } from './panels';

/**
 * Per-camera Overview tab — a dense KPI row (status, model, firmware,
 * network) followed by the onboarding status for this camera. Kept
 * intentionally narrow; deeper analytics + zone info live on their own
 * sibling tabs so the landing view loads fast.
 */
export function cameraOverviewScene(serial: string): EmbeddedScene {
  const kpiItems = cameraOverviewKpiRow(serial).map(
    (panel) => new SceneCSSGridItem({ body: panel })
  );

  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
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
        new SceneFlexItem({
          minHeight: 320,
          body: cameraOnboardingTable(),
        }),
      ],
    }),
  });
}
