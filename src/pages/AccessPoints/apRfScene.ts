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
import { orgVariable } from '../../scene-helpers/variables';
import { apRfPanels } from './panels';

/**
 * Per-AP RF tab — one channel-utilisation timeseries per Wi-Fi band. Each
 * panel carries a `HideWhenEmpty` behavior so silent bands collapse instead
 * of leaving dead chart real estate on single-band APs. The per-band filter
 * is passed through the query contract (`band` field on MerakiQuery) — no
 * user-facing picker, since the three fixed panels already decompose the
 * view by band.
 */
export function apRfScene(serial: string): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [orgVariable()],
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
