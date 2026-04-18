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
import { eventsTable, eventsTimelineBarChart } from './panels';
import { eventTypeVariable, productTypeVariable } from './variables';

/**
 * Events scene — network-scoped live event feed. Layout:
 *   1. Config-not-set banner (collapses when configured).
 *   2. Timeline bar chart: event volume bucketed by time, stacked by
 *      category.
 *   3. Full events table with per-row device drilldown.
 *
 * Variables:
 *  - `$org`, `$network` — standard cascade.
 *  - `$productType` — required by the Meraki API when the network spans
 *    multiple product families. Defaults to `wireless`; users can switch.
 *  - `$eventType` — optional multi-select filter forwarded through
 *    `q.Metrics` → `includedEventTypes[]`.
 *
 * Time range defaults to 24h — events are a diagnostics surface and a
 * longer default window matches the alerts scene.
 */
export function eventsScene(): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-24h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [
        orgVariable(),
        networkVariable(),
        productTypeVariable(),
        eventTypeVariable(),
      ],
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
        configGuardFlexItem(),
        new SceneFlexItem({
          height: 280,
          body: eventsTimelineBarChart(),
        }),
        new SceneFlexItem({
          minHeight: 520,
          body: eventsTable(),
        }),
      ],
    }),
  });
}
