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
import { networkVariableForProductTypes } from '../../scene-helpers/variables';
import { vpnPeerHeatmap, vpnPeerStatsTable } from './panels';

/**
 * Per-appliance VPN tab — a peer heatmap sits above the aggregated stats
 * table. v0.5 §4.4.3-1c REPLACED the previous peer-matrix table with the
 * heatmap so larger AutoVPN meshes stay readable; the stats table is
 * retained for small meshes / per-pair detail.
 *
 * The scene inherits $org from the app shell and exposes a multi-select
 * $network variable so operators can scope the heatmap to a single site;
 * leaving it on "All" shows every peer across the org.
 *
 * This scene receives the selected appliance's serial for API symmetry
 * with the other detail tabs, but the VPN frames are emitted per-network
 * rather than per-device — so the serial is currently unused. Kept in the
 * signature because a later "highlight this appliance's peers" filter will
 * need it.
 */
export function applianceVpnScene(_serial: string): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-1h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [networkVariableForProductTypes(['appliance'])],
    }),
    controls: [
      new VariableValueSelectors({}),
      new SceneControlsSpacer(),
      new SceneTimePicker({ isOnCanvas: true }),
      new SceneRefreshPicker({ intervals: ['30s', '1m', '5m'], isOnCanvas: true }),
    ],
    body: new SceneFlexLayout({
      direction: 'column',
      children: [
        new SceneFlexItem({
          minHeight: 380,
          body: vpnPeerHeatmap(),
        }),
        new SceneFlexItem({
          minHeight: 320,
          body: vpnPeerStatsTable(),
        }),
      ],
    }),
  });
}
