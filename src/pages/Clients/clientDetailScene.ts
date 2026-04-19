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
import {
  clientVariable,
  networkVariable,
  orgVariable,
} from '../../scene-helpers/variables';
import { configGuardFlexItem } from '../../scene-helpers/ConfigGuard';
import {
  clientLatencyTrend,
  clientSearchTable,
  sessionHistoryTimeseries,
} from './panels';

/**
 * Per-client drilldown scene. The MAC arrives via the route param and seeds
 * the `$client` TextBoxVariable so every panel on the detail page reuses the
 * standard `metrics: ['$client']` / `mac=$client` plumbing. The picker stays
 * editable — operators can paste a different MAC without going back to the
 * Search tab.
 *
 * Layout (top-down):
 *   1. Config guard banner.
 *   2. KPI tile (recent latency) + identity table (clientSearchTable).
 *   3. Full-width session-latency timeseries.
 */
export function clientDetailScene(mac: string): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-7d', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [
        orgVariable(),
        networkVariable(),
        clientVariable({ name: 'client', label: 'MAC', value: mac }),
      ],
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
          height: 240,
          body: new SceneFlexLayout({
            direction: 'row',
            children: [
              new SceneFlexItem({ width: 240, body: clientLatencyTrend() }),
              new SceneFlexItem({ body: clientSearchTable() }),
            ],
          }),
        }),
        new SceneFlexItem({ minHeight: 480, body: sessionHistoryTimeseries() }),
      ],
    }),
  });
}
