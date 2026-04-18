import React from 'react';
import {
  EmbeddedScene,
  SceneControlsSpacer,
  SceneFlexItem,
  SceneFlexLayout,
  SceneReactObject,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
} from '@grafana/scenes';
import {
  ALL_SENSOR_METRICS,
  sensorDetailLatestReadings,
  sensorDetailMetricPanel,
} from '../../scene-helpers/panels';
import { orgOnlyVariables } from '../../scene-helpers/variables';
import { SensorMetadata } from './SensorMetadata';
import { HideWhenEmpty } from './behaviors';

/**
 * Per-sensor detail — one metric panel per Meraki metric, plus the latest
 * readings table and a metadata card at the top. Panels auto-hide when the
 * sensor doesn't report that metric (HideWhenEmpty behavior).
 */
export function sensorDetailScene(serial: string) {
  const metricItems = ALL_SENSOR_METRICS.map((meta) => {
    const panel = sensorDetailMetricPanel(serial, meta);
    return new SceneFlexItem({
      minHeight: meta.discrete ? 160 : 240,
      body: panel,
      $behaviors: [new HideWhenEmpty()],
    });
  });

  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-24h', to: 'now' }),
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
          minHeight: 220,
          body: new SceneReactObject({
            component: () => React.createElement(SensorMetadata, { serial }),
          }),
        }),
        new SceneFlexItem({
          height: 280,
          body: sensorDetailLatestReadings(serial),
        }),
        ...metricItems,
      ],
    }),
  });
}
