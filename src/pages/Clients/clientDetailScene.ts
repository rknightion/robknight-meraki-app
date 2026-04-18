import {
  CustomVariable,
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
  clientLatencyTrend,
  clientSearchTable,
  sessionHistoryTimeseries,
} from './panels';

/**
 * Per-client drilldown scene. The MAC arrives via the route param and is
 * baked into a one-shot CustomVariable named `$client` so every panel on
 * the detail page reuses the standard `metrics: ['$client']` / `mac=$client`
 * plumbing without reading the URL directly.
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
        new CustomVariable({
          name: 'client',
          label: 'MAC',
          // The CustomVariable's `text`/`value` are what panels see; the
          // `query` field is the picker's free-form options. Setting all
          // three to the route-param MAC pins the variable to the URL
          // identity while keeping the picker editable.
          query: mac,
          value: mac,
          text: mac,
          isMulti: false,
          includeAll: false,
        }),
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
