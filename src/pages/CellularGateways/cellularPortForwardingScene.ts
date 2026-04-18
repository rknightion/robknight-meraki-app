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
import { mgPortForwardingTable } from './panels';

/**
 * Per-gateway Port Forwarding tab — a single full-width table of inbound
 * rules. Kept minimal; LAN-side fixed IPs and reserved ranges live on
 * the overview scene's companion {@link mgLanPanel} once we route them in
 * (future enhancement — kept scope narrow for v0.3.0).
 */
export function cellularPortForwardingScene(serial: string): EmbeddedScene {
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
          minHeight: 520,
          body: mgPortForwardingTable(serial),
        }),
      ],
    }),
  });
}
