import {
  EmbeddedScene,
  SceneControlsSpacer,
  SceneFlexItem,
  SceneFlexLayout,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
  SceneVariableSet,
  VariableValueSelectors,
} from '@grafana/scenes';
import { orgVariable } from '../../scene-helpers/variables';
import { configGuardFlexItem } from '../../scene-helpers/ConfigGuard';
import { networkLinkGraphPanel } from './panels';
import { topologyNetworkVariable } from './variables';

/**
 * Topology / Network Map (v0.5 §4.4.4-D).
 *
 * A single full-page LLDP/CDP device link graph scoped to the selected
 * `$network`. Single-select is load-bearing: the backend caps the
 * per-network device fan-out so org-wide expansion would blow through
 * the rate budget.
 *
 * The scene is snapshot-oriented (no native timeseries) so the time
 * picker acts as a "force refresh" affordance — `deviceLldpCdp` has a
 * 15 m cache TTL so even a frequent refresh stays inside the window.
 */
export function topologyScene(): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [orgVariable(), topologyNetworkVariable()],
    }),
    controls: [
      new VariableValueSelectors({}),
      new SceneControlsSpacer(),
      new SceneTimePicker({ isOnCanvas: true }),
      new SceneRefreshPicker({ intervals: ['1m', '5m', '15m'], isOnCanvas: true }),
    ],
    body: new SceneFlexLayout({
      direction: 'column',
      children: [
        configGuardFlexItem(),
        new SceneFlexItem({
          minHeight: 900,
          body: networkLinkGraphPanel(),
        }),
      ],
    }),
  });
}
