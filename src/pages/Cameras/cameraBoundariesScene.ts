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
import { cameraBoundariesTable } from './panels';

/**
 * Per-camera Boundaries tab — one table listing every area + line boundary
 * configured on this camera (boundaryId / name / kind / type). Intentionally
 * minimal; the per-boundary detection timeseries lives on the Analytics tab
 * so this stays focused on "what boundaries does this camera have?".
 */
export function cameraBoundariesScene(serial: string): EmbeddedScene {
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
        new SceneFlexItem({
          minHeight: 520,
          body: cameraBoundariesTable(serial),
        }),
      ],
    }),
  });
}
