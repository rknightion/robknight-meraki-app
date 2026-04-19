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
  portDetailKpiStats,
  portDetailNeighborPanel,
  switchPortConfigPanel,
  switchPortErrorsSnapshot,
  switchPortPacketCountersPanel,
} from './panels';

/**
 * Per-port detail — packet counters (rx/tx + derived rates), port config,
 * and error snapshot. v0.8 adds a KPI row at the top (live STP state,
 * active profile, current traffic rate, PoE draw — all filtered client-
 * side from the widened switch_ports frame) plus a neighbour panel.
 *
 * No time series for the packet counters: the Meraki endpoint only returns
 * a cumulative snapshot. A future ring-buffer kind (todos.txt 1.9) would
 * unlock timeseries panels here.
 */
export function portDetailScene(serial: string, portId: string): EmbeddedScene {
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
        // v0.8 — port KPI strip. Reuses the switch_ports frame, filtered to
        // this (serial, portId) client-side, so the backend call is shared
        // with the Ports tab's port map on the same refresh.
        new SceneFlexItem({
          minHeight: 120,
          body: portDetailKpiStats(serial, portId),
        }),
        // v0.8 — LLDP/CDP neighbour scoped to this port.
        new SceneFlexItem({
          minHeight: 180,
          body: portDetailNeighborPanel(serial, portId),
        }),
        new SceneFlexItem({
          minHeight: 260,
          body: switchPortPacketCountersPanel(serial, portId),
        }),
        new SceneFlexItem({
          minHeight: 220,
          body: switchPortConfigPanel(serial, portId),
        }),
        // §4.4.3-1b panel #7 — port-error snapshot. Reshapes the existing
        // packet-counters frame via a filterByValue on `desc` to show only
        // the error-family buckets (CRC align errors, Collisions, etc.).
        new SceneFlexItem({
          minHeight: 220,
          body: switchPortErrorsSnapshot(serial, portId),
        }),
      ],
    }),
  });
}
