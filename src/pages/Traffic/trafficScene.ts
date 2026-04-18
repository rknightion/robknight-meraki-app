import {
  EmbeddedScene,
  SceneControlsSpacer,
  SceneFlexItem,
  SceneFlexLayout,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
  VariableValueSelectors,
} from '@grafana/scenes';
import { configGuardFlexItem } from '../../scene-helpers/ConfigGuard';
import { trafficGuardFlexItem } from '../../scene-helpers/TrafficGuard';
import {
  networkTrafficTable,
  topApplicationCategoriesTable,
  topApplicationsBarChart,
  topApplicationsTable,
} from './panels';
import { trafficVariables } from './variables';

/**
 * Traffic Analytics scene — L7 application breakdown across the selected
 * organisation and networks.
 *
 * Layout, top-down:
 *   1. Config-guard banner (hidden when the plugin is configured).
 *   2. TrafficGuard banner (hidden unless one or more selected networks
 *      have traffic analysis disabled).
 *   3. Top apps row: bar chart + table side-by-side (height 360).
 *   4. Top categories table (height 360).
 *   5. Per-network traffic table (full width, min height 420).
 *
 * Default time range is 24h — the per-network endpoint caps at 30 days and
 * the org-level summaries default to 1 day, so 24h matches the natural
 * granularity. Users can broaden the picker; the backend clamps to the
 * Meraki cap automatically via KnownEndpointRanges.
 */
export function trafficScene(): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-24h', to: 'now' }),
    $variables: trafficVariables(),
    controls: [
      new VariableValueSelectors({}),
      new SceneControlsSpacer(),
      new SceneTimePicker({ isOnCanvas: true }),
      new SceneRefreshPicker({ intervals: ['30s', '1m', '5m'], isOnCanvas: true }),
    ],
    body: new SceneFlexLayout({
      direction: 'column',
      children: [
        configGuardFlexItem(),
        trafficGuardFlexItem(),
        // Row 1: top applications — bar chart on the left, table on the right.
        new SceneFlexItem({
          height: 360,
          body: new SceneFlexLayout({
            direction: 'row',
            children: [
              new SceneFlexItem({ body: topApplicationsBarChart() }),
              new SceneFlexItem({ body: topApplicationsTable() }),
            ],
          }),
        }),
        // Row 2: top categories — single full-width table.
        new SceneFlexItem({
          height: 360,
          body: topApplicationCategoriesTable(),
        }),
        // Row 3: per-network traffic mix — minHeight so the page can grow
        // with row count on busy estates.
        new SceneFlexItem({
          minHeight: 420,
          body: networkTrafficTable(),
        }),
      ],
    }),
  });
}
