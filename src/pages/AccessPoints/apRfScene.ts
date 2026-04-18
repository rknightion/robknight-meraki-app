import {
  EmbeddedScene,
  SceneControlsSpacer,
  SceneFlexLayout,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
  SceneVariableSet,
  VariableValueSelectors,
} from '@grafana/scenes';
import { apRfPanels } from './panels';
import { wirelessBandVariable } from './variables';

/**
 * Per-AP RF tab — one channel-utilisation timeseries per Wi-Fi band. Each
 * panel carries a `HideWhenEmpty` behavior so silent bands collapse instead
 * of leaving dead chart real estate on single-band APs. The `$band` variable
 * is exposed so users can force a single-band view across the stack.
 */
export function apRfScene(serial: string): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [wirelessBandVariable()],
    }),
    controls: [
      new VariableValueSelectors({}),
      new SceneControlsSpacer(),
      new SceneTimePicker({ isOnCanvas: true }),
      new SceneRefreshPicker({ intervals: ['30s', '1m', '5m'], isOnCanvas: true }),
    ],
    body: new SceneFlexLayout({
      direction: 'column',
      children: apRfPanels(serial),
    }),
  });
}
