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
import { mgOverviewKpiRow, mgUplinkTable } from './panels';

/**
 * Per-gateway Overview tab — a dense KPI row (status, model, firmware,
 * network) followed by the per-device uplink detail table. Sibling tabs
 * host the signal gauges and port-forwarding rules so the landing view
 * loads fast.
 */
export function cellularOverviewScene(serial: string): EmbeddedScene {
  const kpiItems = mgOverviewKpiRow(serial).map(
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
          body: mgUplinkTable(serial),
        }),
      ],
    }),
  });
}
