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
 *  - `$org` — organisation.
 *  - `$admin` — free-form admin-id/email filter forwarded through
 *    `q.Metrics[0]`. Empty string means "no filter".
 *
 * No $network picker: Meraki's /configurationChanges endpoint excludes
 * org-level changes (the majority of audit entries) whenever a networkId
 * filter is applied, which made the panel render empty on orgs whose
 * recent changes were all org-scoped. The admin/time filters cover the
 * realistic narrow-down workflows without that foot-gun.
 *
 * Time range defaults to 24h — audit surfaces benefit from a slightly
 * wider default window than a live metrics page.
 */
export function auditLogScene(): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-24h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [orgVariable(), adminVariable()],
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
