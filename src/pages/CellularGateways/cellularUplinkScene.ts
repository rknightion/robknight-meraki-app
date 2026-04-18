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
import { mgSignalGauge, mgUplinkTable } from './panels';

/**
 * Per-gateway Uplink tab — two side-by-side signal-strength gauges (RSRP,
 * RSRQ) plus the full uplink table. The gauges give an instant read on
 * whether the cell is marginal; the table below surfaces the detail fields
 * (provider, APN, ICCID, public IP).
 */
export function cellularUplinkScene(serial: string): EmbeddedScene {
  const gaugeItems = [
    new SceneCSSGridItem({ body: mgSignalGauge(serial, 'rsrpDb') }),
    new SceneCSSGridItem({ body: mgSignalGauge(serial, 'rsrqDb') }),
  ];

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
          height: 220,
          body: new SceneCSSGridLayout({
            templateColumns: 'repeat(auto-fit, minmax(260px, 1fr))',
            autoRows: '200px',
            rowGap: 1,
            columnGap: 1,
            children: gaugeItems,
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
