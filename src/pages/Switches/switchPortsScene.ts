import {
  EmbeddedScene,
  SceneControlsSpacer,
  SceneFlexItem,
  SceneFlexLayout,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
} from '@grafana/scenes';
import { switchPortMap } from './panels';

/**
 * Ports tab for a single switch — the port map table in a tall flex item.
 * The port-ID column drilldowns into the per-port detail page (packet
 * counters + config summary) via the wildcard route on `switchDetailPage`.
 */
export function switchPortsScene(serial: string): EmbeddedScene {
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
          minHeight: 640,
          body: switchPortMap(serial),
        }),
      ],
    }),
  });
}
