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
  orgDevicesTable,
  orgNetworksTable,
} from '../../scene-helpers/panels';

/**
 * Per-organization detail — KPI row, status donut, networks + devices
 * tables. Devices table serial column deep-links into the sensor detail
 * scene (used for MTs; non-sensor devices will show a broken link until
 * the MR/MS/MX pages land in v0.2+).
 */
export function organizationDetailScene(orgId: string) {
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
        new SceneFlexItem({
          minHeight: 420,
          body: orgDevicesTable(orgId),
        }),
      ],
    }),
  });
}
