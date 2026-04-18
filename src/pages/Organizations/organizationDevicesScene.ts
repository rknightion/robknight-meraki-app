import {
  EmbeddedScene,
  SceneControlsSpacer,
  SceneFlexItem,
  SceneFlexLayout,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
} from '@grafana/scenes';
import { orgDevicesTable } from '../../scene-helpers/panels';

/**
 * Devices tab for a single organization — the full devices inventory
 * table. Serial drilldowns into the sensor detail scene (same behavior
 * as the previous flat org-detail page); non-sensor device links will
 * light up when the MR/MS drilldowns land in later waves.
 */
export function organizationDevicesScene(orgId: string): EmbeddedScene {
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
          minHeight: 420,
          body: orgDevicesTable(orgId),
        }),
      ],
    }),
  });
}
