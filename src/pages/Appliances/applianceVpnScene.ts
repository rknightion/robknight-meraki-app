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
import { vpnPeerMatrixTable, vpnPeerStatsTable } from './panels';

/**
 * Per-appliance VPN tab — the peer matrix sits above the aggregated stats
 * table. The tab inherits $org from the app shell and exposes a
 * multi-select $network variable so operators can scope the matrix to a
 * single site; leaving it on "All" shows every peer across the org.
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
          minHeight: 360,
          body: vpnPeerMatrixTable(),
        }),
        new SceneFlexItem({
          minHeight: 320,
          body: vpnPeerStatsTable(),
        }),
      ],
    }),
  });
}
