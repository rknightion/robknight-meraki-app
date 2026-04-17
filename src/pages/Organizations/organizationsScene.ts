import {
  EmbeddedScene,
  SceneControlsSpacer,
  SceneFlexItem,
  SceneFlexLayout,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
} from '@grafana/scenes';
import { orgInventoryTable } from '../../scene-helpers/panels';

/**
 * Organizations overview — a single inventory table with clickable rows
 * that drill into the per-org detail scene. Kept deliberately lightweight;
 * KPI summaries live on the detail view where we have an `orgId` context.
 */
export function organizationsScene() {
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
        new SceneFlexItem({ minHeight: 520, body: orgInventoryTable() }),
      ],
    }),
  });
}
