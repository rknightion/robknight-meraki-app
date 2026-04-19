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
import { switchMacAddressTable, switchPortMap } from './panels';

/**
 * Ports tab for a single switch — the port map table and MAC-address
 * table. The port-ID column on the port map drilldowns into the per-port
 * detail page (packet counters + config summary + port-error snapshot)
 * via the wildcard route on `switchDetailPage`.
 *
 * STP history note: the switchStpTopologyTable panel used to live here,
 * passing the literal string `$network` as its networkId because the
 * switch detail page has no network variable in scope — it always
 * rendered empty with "No STP settings configured for this network"
 * even on networks that had STP enabled. STP is network-scoped (not
 * switch-scoped) so the panel belongs on a network detail page; removed
 * 2026-04-19 after operator feedback flagged it as misleading.
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
      ],
    }),
  });
}
