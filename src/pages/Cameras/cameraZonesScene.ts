import {
  EmbeddedScene,
  SceneControlsSpacer,
  SceneFlexItem,
  SceneFlexLayout,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
} from '@grafana/scenes';
import { cameraZonesTable } from './panels';

/**
 * Per-camera Zones tab — one table listing every analytics zone configured
 * on this camera (zoneId / type / label). Intentionally minimal; the zone
 * history timeseries lives on the Analytics tab so this stays focused on
 * "what zones does this camera have?".
 */
export function cameraZonesScene(serial: string): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
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
          body: cameraZonesTable(serial),
        }),
      ],
    }),
  });
}
