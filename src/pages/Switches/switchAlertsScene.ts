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
import { configGuardFlexItem } from '../../scene-helpers/ConfigGuard';
import { switchAlertsTable } from './panels';

/**
 * Alerts tab for a single switch — renders the assurance-alerts feed
 * filtered to this serial. Reuses the backend KindAlerts query-kind with a
 * `serials` filter, so no new API calls beyond what the global Alerts page
 * already hits.
 *
 * Default window mirrors the other detail tabs (6 hours) — operators can
 * broaden via the time picker if they need historical context on a
 * recurring alert.
 */
export function switchAlertsScene(serial: string): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
    // Declare `$org` so it hydrates from var-org on drilldowns; without this
    // the orgId field ships as literal "$org" and the backend 400s.
    $variables: orgOnlyVariables(),
    controls: [
      new SceneControlsSpacer(),
      new SceneTimePicker({ isOnCanvas: true }),
      new SceneRefreshPicker({ intervals: ['30s', '1m', '5m'], isOnCanvas: true }),
    ],
    body: new SceneFlexLayout({
      direction: 'column',
      children: [
        configGuardFlexItem(),
        new SceneFlexItem({
          minHeight: 520,
          body: switchAlertsTable(serial),
        }),
      ],
    }),
  });
}
