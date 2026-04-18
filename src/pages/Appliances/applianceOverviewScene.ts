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
  applianceOverviewKpiRow,
  applianceUplinkStatusTable,
} from './panels';

/**
 * Per-appliance Overview tab — KPI row (status / model / firmware /
 * network) on top of a per-serial uplink status table. Mirrors the shape of
 * `apOverviewScene` and `switchOverviewScene`; the richer content
 * (loss/latency timeseries, VPN peers, firewall) lives on sibling tabs.
 */
export function applianceOverviewScene(serial: string): EmbeddedScene {
  const kpiItems = applianceOverviewKpiRow(serial).map(
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
          body: applianceUplinkStatusTable(serial),
        }),
      ],
    }),
  });
}
