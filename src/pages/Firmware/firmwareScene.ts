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
import { networkVariable, orgVariable } from '../../scene-helpers/variables';
import { configGuardFlexItem } from '../../scene-helpers/ConfigGuard';
import {
  deviceEolTable,
  firmwareEolSoonCountStat,
  firmwarePendingCountStat,
  firmwarePendingTable,
  firmwareScheduledCountStat,
} from './panels';
import { eoxStatusVariable } from './variables';

/**
 * Firmware & Lifecycle scene — org-scoped firmware-upgrade visibility plus
 * Meraki-published end-of-life / end-of-sale tracking.
 *
 * Layout (per the §4.4.4-B execution plan, step 4):
 *
 *   1. Config-not-set banner (collapses when configured).
 *   2. KPI row — three stat tiles:
 *        - Upgrades scheduled (org-wide /firmware/upgrades, status=scheduled)
 *        - Devices pending upgrade (per-device, currentUpgradesOnly=true)
 *        - EOL ≤ 90 days (inventory eoxStatus + daysUntil filter)
 *   3. Pending upgrades table — one row per device with a current/in-progress
 *      upgrade. `daysUntil` is threshold-coloured (<7d red, <30d amber).
 *   4. EOL devices table — one row per EOX-flagged device, sorted by
 *      daysUntil ascending so already-past-EOS rows float to the top.
 *
 * Variables:
 *  - `$org`       — standard org cascade.
 *  - `$network`   — multi-network filter (cascades from $org).
 *  - `$eoxStatus` — single-select EOX bucket filter for the EOL table
 *                   (default empty = all three buckets).
 *
 * Time range: firmware state is slow-moving and the page surfaces snapshot
 * data (current scheduled upgrades + current EOL list) — the time picker
 * is wired in for parity with the rest of the app, but the underlying
 * handlers ignore it. Default 7d window matches the rough upgrade-window
 * cadence operators care about.
 */
export function firmwareScene(): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-7d', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [orgVariable(), networkVariable(), eoxStatusVariable()],
    }),
    controls: [
      new VariableValueSelectors({}),
      new SceneControlsSpacer(),
      new SceneTimePicker({ isOnCanvas: true }),
      new SceneRefreshPicker({ intervals: ['5m', '15m', '30m'], isOnCanvas: true }),
    ],
    body: new SceneFlexLayout({
      direction: 'column',
      children: [
        configGuardFlexItem(),
        // Row 1 — KPI tiles. Each stat is its own SceneFlexItem so they
        // size evenly across the row regardless of viewport width.
        new SceneFlexLayout({
          direction: 'row',
          height: 140,
          children: [
            new SceneFlexItem({ body: firmwareScheduledCountStat() }),
            new SceneFlexItem({ body: firmwarePendingCountStat() }),
            new SceneFlexItem({ body: firmwareEolSoonCountStat() }),
          ],
        }),
        // Row 2 — pending upgrades table.
        new SceneFlexItem({
          minHeight: 360,
          body: firmwarePendingTable(),
        }),
        // Row 3 — EOL devices table.
        new SceneFlexItem({
          minHeight: 420,
          body: deviceEolTable(),
        }),
      ],
    }),
  });
}
