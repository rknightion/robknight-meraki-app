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
import { networkGeomapPanel, networkLinkGraphPanel } from './panels';
import { topologyNetworkVariable } from './variables';

/**
 * Topology / Network Map (v0.5 §4.4.4-D).
 *
 *   Row 1: Org-wide geomap of networks (one marker per network).
 *   Row 2: Per-network LLDP/CDP link graph for the selected $network.
 *
 * The page uses a single-select `$network` variable rather than the
 * shared multi-select `networkVariable()` because Row 2's per-device
 * fan-out is gated to one network at a time. Row 1 is org-scoped via
 * `$org` and ignores the network filter.
 *
 * The scene is snapshot-oriented (no native timeseries) so we use a
 * narrow time picker mostly as a "force refresh" affordance rather than
 * a true range selector. The `networkGeo` data has a 1 h TTL and
 * `deviceLldpCdp` 15 m so even a frequent refresh stays inside the
 * cache window.
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
          minHeight: 480,
          body: networkGeomapPanel(),
        }),
        new SceneFlexItem({
          minHeight: 520,
          body: networkLinkGraphPanel(),
        }),
      ],
    }),
  });
}
