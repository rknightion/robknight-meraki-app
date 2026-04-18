import {
  EmbeddedScene,
  SceneControlsSpacer,
  SceneFlexItem,
  SceneFlexLayout,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
} from '@grafana/scenes';
import { uplinkLossLatencyTimeseries } from './panels';

/**
 * Per-appliance Uplinks tab — stacked timeseries for packet loss and
 * latency per uplink. Default time range is `now-5m/now` because the
 * underlying Meraki endpoint caps at a 5-minute probe window; operators can
 * still widen it via the time picker to get the full 31-day history that
 * the backend resolver allows.
 */
export function applianceUplinksScene(serial: string): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-5m', to: 'now' }),
    controls: [
      new SceneControlsSpacer(),
      new SceneTimePicker({ isOnCanvas: true }),
      new SceneRefreshPicker({ intervals: ['30s', '1m', '5m'], isOnCanvas: true }),
    ],
    body: new SceneFlexLayout({
      direction: 'column',
      children: [
        new SceneFlexItem({
          minHeight: 320,
          body: uplinkLossLatencyTimeseries(serial, 'lossPercent'),
        }),
        new SceneFlexItem({
          minHeight: 320,
          body: uplinkLossLatencyTimeseries(serial, 'latencyMs'),
        }),
      ],
    }),
  });
}
