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
  cameraBoundariesTable,
  cameraDetectionsTimeseries,
} from './panels';
import { cameraBoundaryVariable, cameraObjectTypeVariable } from './variables';

/**
 * Per-camera Analytics tab — boundary-based detection counts plus a table of
 * configured boundaries.
 *
 * Time range defaults to 24h; Meraki's detections endpoint aggregates its
 * returned window around the current time, so a longer dashboard range gives
 * operators useful context.
 *
 * Variables:
 *  - `$objectType` — person / vehicle switch shared across the detections
 *    chart.
 *  - `$boundary` — scoped to this camera (serial baked in via the variable's
 *    `serials: [serial]`) so operators can narrow the detections chart to
 *    one boundary at a time.
 */
export function cameraAnalyticsScene(serial: string): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-24h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [cameraObjectTypeVariable(), cameraBoundaryVariable(serial)],
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
          height: 360,
          body: cameraDetectionsTimeseries(serial),
        }),
        new SceneFlexItem({
          minHeight: 260,
          body: cameraBoundariesTable(serial),
        }),
      ],
    }),
  });
}
