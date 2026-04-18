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
import { orgVariable } from '../../scene-helpers/variables';
import {
  uplinkLossLatencyHistoryTimeseries,
  uplinkLossLatencyTimeseries,
} from './panels';
import { mxVariable } from './variables';

/**
 * Per-appliance Uplinks tab — stacked timeseries for:
 *  - 5-minute snapshot panels (per hardcoded serial from the drilldown URL).
 *  - 31-day history panels using the `$mx` picker so operators can compare
 *    the full history window on any appliance without leaving the tab.
 *
 * Default time range is `now-5m/now` for the snapshot panels; operators can
 * widen it via the time picker to drive the history panels up to 31 days.
 */
export function applianceUplinksScene(serial: string): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-5m', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [
        orgVariable(),
        mxVariable(),
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
        // 5-minute snapshot (per drilldown serial).
        new SceneFlexItem({
          minHeight: 320,
          body: uplinkLossLatencyTimeseries(serial, 'lossPercent'),
        }),
        new SceneFlexItem({
          minHeight: 320,
          body: uplinkLossLatencyTimeseries(serial, 'latencyMs'),
        }),
        // 31-day history panels side by side (driven by $mx variable).
        new SceneFlexItem({
          minHeight: 320,
          body: new SceneFlexLayout({
            direction: 'row',
            children: [
              new SceneFlexItem({
                body: uplinkLossLatencyHistoryTimeseries('lossPercent'),
              }),
              new SceneFlexItem({
                body: uplinkLossLatencyHistoryTimeseries('latencyMs'),
              }),
            ],
          }),
        }),
      ],
    }),
  });
}
