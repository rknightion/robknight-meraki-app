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
import {
  orgDetailKpiRow,
  orgDeviceStatusDonut,
  orgNetworksTable,
} from '../../scene-helpers/panels';

/**
 * Overview tab for a single organization — the KPI row, device-status
 * donut, and networks table. The devices and alerts tables move to their
 * own tabs so this page stays focused on "how is this org doing?".
 */
export function organizationOverviewScene(orgId: string): EmbeddedScene {
  const kpiItems = orgDetailKpiRow(orgId).map(
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
          height: 120,
          body: new SceneCSSGridLayout({
            templateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
            autoRows: '100px',
            rowGap: 1,
            columnGap: 1,
            children: kpiItems,
          }),
        }),
        new SceneFlexItem({
          height: 300,
          body: orgDeviceStatusDonut(orgId),
        }),
        new SceneFlexItem({
          minHeight: 360,
          body: orgNetworksTable(orgId),
        }),
      ],
    }),
  });
}
