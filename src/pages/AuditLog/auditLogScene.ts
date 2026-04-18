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
import { auditLogTable, auditLogTimelineBarChart } from './panels';
import { adminVariable } from './variables';

/**
 * Audit Log scene — org-scoped configuration-change history. Layout:
 *   1. Config-not-set banner (collapses when configured).
 *   2. Change-volume timeline bar chart.
 *   3. Full configuration-changes table.
 *
 * Variables:
 *  - `$org`, `$network` — standard cascade.
 *  - `$admin` — free-form admin-id/email filter forwarded through
 *    `q.Metrics[0]`. Empty string means "no filter".
 *
 * Time range defaults to 24h — audit surfaces benefit from a slightly
 * wider default window than a live metrics page.
 */
export function auditLogScene(): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-24h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [orgVariable(), networkVariable(), adminVariable()],
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
          height: 280,
          body: auditLogTimelineBarChart(),
        }),
        new SceneFlexItem({
          minHeight: 520,
          body: auditLogTable(),
        }),
      ],
    }),
  });
}
