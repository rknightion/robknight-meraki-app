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
import {
  switchMacAddressTable,
  switchPortMap,
  switchStpTopologyTable,
} from './panels';

/**
 * Ports tab for a single switch — the port map table, MAC-address table,
 * and a per-network STP topology snapshot. The port-ID column on the port
 * map drilldowns into the per-port detail page (packet counters + config
 * summary + port-error snapshot) via the wildcard route on
 * `switchDetailPage`.
 *
 * STP is network-scoped; we pass `$network` so callers must have the
 * network variable in scope (via `orgOnlyVariables()` today the panel will
 * render empty — see panel `setNoValue` copy). Additive in §4.4.3-1b; the
 * port map remains the primary panel.
 */
export function switchPortsScene(serial: string): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
    // Declare `$org` so the scene hydrates from the `var-org` query-param
    // carried by the drilldown link. Without this the panels ship
    // `orgId: '$org'` literally and the backend 400s.
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
          minHeight: 480,
          body: switchPortMap(serial),
        }),
        new SceneFlexItem({
          minHeight: 280,
          body: switchMacAddressTable(serial),
        }),
        new SceneFlexItem({
          minHeight: 200,
          body: switchStpTopologyTable('$network'),
        }),
      ],
    }),
  });
}
