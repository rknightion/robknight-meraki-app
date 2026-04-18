import {
  EmbeddedScene,
  SceneControlsSpacer,
  SceneFlexItem,
  SceneFlexLayout,
  SceneReactObject,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
  SceneVariableSet,
  VariableValueSelectors,
} from '@grafana/scenes';
import { HomeIntro } from './HomeIntro';
import { orgVariable } from '../../scene-helpers/variables';
import { orgDeviceStatusDonut, orgInventoryTable } from '../../scene-helpers/panels';
import { configGuardFlexItem } from '../../scene-helpers/ConfigGuard';
import { recentAlertsTile } from '../Alerts/panels';
import {
  HOME_AT_A_GLANCE_KPIS,
  availabilityByFamilyStackedBar,
  homeAtAGlanceStats,
  orgChangeFeedTile,
} from './panels';

/**
 * Home layout per v0.5 §4.4.5:
 *   Row 1 — ConfigGuard banner (renders only when the plugin is unconfigured).
 *   Row 2 — HomeIntro condensed to a single-line hint.
 *   Row 3 — 6-stat "At a glance" KPI row (orgHealthSummary fan-out).
 *   Row 4 — Issues feed: recent alerts + 24h change feed.
 *   Row 5 — Availability breakdown: device-status donut + by-family stacked bar.
 *   Row 6 — Org inventory table (drill-out surface).
 */
export function homeScene() {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
    $variables: new SceneVariableSet({ variables: [orgVariable()] }),
    controls: [
      new VariableValueSelectors({}),
      new SceneControlsSpacer(),
      new SceneTimePicker({ isOnCanvas: true }),
      new SceneRefreshPicker({ intervals: ['30s', '1m', '5m'], isOnCanvas: true }),
    ],
    body: new SceneFlexLayout({
      direction: 'column',
      children: [
        // Row 1 — ConfigGuard.
        configGuardFlexItem(),

        // Row 2 — HomeIntro (condensed to a ~40 px single-line banner; the
        // full welcome block + nav CTA grid was removed in §4.4.5 because the
        // Grafana sidebar already owns navigation).
        new SceneFlexItem({
          height: 40,
          body: new SceneReactObject({ component: HomeIntro }),
        }),

        // Row 3 — At-a-glance 6-stat KPI row. Each tile is one field of the
        // orgHealthSummary wide frame; thresholds match §4.4.4-E defaults.
        new SceneFlexLayout({
          direction: 'row',
          height: 100,
          children: homeAtAGlanceStats('$org').map(
            (panel) =>
              new SceneFlexItem({
                minWidth: 140,
                body: panel,
              })
          ),
        }),

        // Row 4 — Issues feed. Two cells: recent alerts (existing) + the
        // polished 24h change feed tile (§4.4.3-1f promoted out of stub).
        new SceneFlexLayout({
          direction: 'row',
          height: 260,
          children: [
            new SceneFlexItem({ body: recentAlertsTile('$org') }),
            new SceneFlexItem({ body: orgChangeFeedTile('$org') }),
          ],
        }),

        // Row 5 — Availability breakdown. Donut (total) + by-family stacked
        // bar (per productType).
        new SceneFlexLayout({
          direction: 'row',
          height: 280,
          children: [
            new SceneFlexItem({ body: orgDeviceStatusDonut('$org') }),
            new SceneFlexItem({ body: availabilityByFamilyStackedBar('$org') }),
          ],
        }),

        // Row 6 — Org inventory table (drill-out to Organizations page).
        new SceneFlexItem({
          minHeight: 360,
          body: orgInventoryTable(),
        }),
      ],
    }),
  });
}

// Re-export the KPI spec list so Playwright / Jest can enumerate the tiles
// without duplicating the order. Exported here (rather than from panels.ts
// directly) so scene consumers have a single import surface.
export { HOME_AT_A_GLANCE_KPIS };
