import {
  EmbeddedScene,
  SceneCSSGridItem,
  SceneCSSGridLayout,
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
import {
  cotermExpirationStat,
  licenseRenewalCalendar,
  licensesTable,
  licensingKpiRow,
} from './panels';
import { licenseStateVariable } from './variables';

/**
 * Licensing tab — the default landing view for the Insights page.
 *
 * Layout, top-down:
 *   1. Config-guard banner (hidden when the plugin is configured).
 *   2. KPI row: active / expiring / expired / total from `LicensesOverview`.
 *   3. Co-term expiration stat. Shows "—" for per-device orgs (zero-valued
 *      `cotermExpiration` time from the backend) — a one-tile placeholder is
 *      less surprising than a conditional panel appearing only for co-term.
 *   4. Per-license table filtered by `$licenseState`. Threshold colour on
 *      `daysUntilExpiry` highlights rows approaching renewal.
 *
 * No time range variable bindings on the licensing queries — the backend
 * treats the dashboard range as unused for license endpoints (licensing is a
 * point-in-time snapshot). The time picker stays in the controls strip for
 * consistency with the other Insights tabs.
 */
export function licensingScene(): EmbeddedScene {
  const kpiItems = licensingKpiRow().map(
    (panel) => new SceneCSSGridItem({ body: panel })
  );

  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-24h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [orgVariable(), licenseStateVariable()],
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
        configGuardFlexItem(),
        new SceneFlexItem({
          height: 120,
          body: new SceneCSSGridLayout({
            templateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
            autoRows: '100px',
            rowGap: 1,
            columnGap: 1,
            children: kpiItems,
          }),
        }),
        new SceneFlexItem({
          height: 120,
          body: cotermExpirationStat(),
        }),
        // §4.4.3-1f — license renewal calendar: status table with
        // colour-coded daysUntilExpiry cells. Sits above the full inventory
        // so operators see the "who needs renewal" summary first.
        new SceneFlexItem({
          minHeight: 320,
          body: licenseRenewalCalendar(),
        }),
        new SceneFlexItem({
          minHeight: 500,
          body: licensesTable(),
        }),
      ],
    }),
  });
}
