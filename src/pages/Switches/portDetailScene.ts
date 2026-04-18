import {
  EmbeddedScene,
  SceneControlsSpacer,
  SceneFlexItem,
  SceneFlexLayout,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
} from '@grafana/scenes';
import { orgOnlyVariables } from '../../scene-helpers/variables';
import { switchPortConfigPanel, switchPortPacketCountersPanel } from './panels';

/**
 * Per-port detail — a snapshot of packet counters (rx/tx + derived rates)
 * and the port config summary. No time series: the counters endpoint only
 * returns the latest snapshot, not a history. A future ring-buffer kind
 * (todos.txt 1.9) would unlock timeseries panels here.
 */
export function portDetailScene(serial: string, portId: string): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
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
          minHeight: 260,
          body: switchPortPacketCountersPanel(serial, portId),
        }),
        new SceneFlexItem({
          minHeight: 220,
          body: switchPortConfigPanel(serial, portId),
        }),
      ],
    }),
  });
}
