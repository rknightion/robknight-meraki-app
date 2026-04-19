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
} from '@grafana/scenes';
import { orgOnlyVariables } from '../../scene-helpers/variables';
import {
  dhcpSeenServersTable,
  switchL3InterfacesTable,
  switchOverviewKpiRow,
  switchPoeBudgetStat,
  switchStackMembersTable,
  switchVlanDistributionDonut,
} from './panels';

/**
 * Overview tab for a single switch — KPI tiles (status, model, firmware,
 * client count) + PoE draw + VLAN donut. v0.8 adds a two-column row of
 * stack / L3-interfaces context (rendered as "not in a stack" / "no L3
 * interfaces" empty states on L2-only or standalone switches — no feature
 * flags, panels always visible) and a full-width DHCP-servers-seen table
 * (rogue detection) scoped to the switch's network.
 */
export function switchOverviewScene(serial: string): EmbeddedScene {
  const kpiItems = switchOverviewKpiRow(serial).map(
    (panel) => new SceneCSSGridItem({ body: panel })
  );
  // PoE draw sits beside the existing four KPIs — it's a scalar summary of
  // the ports feed we already fetch. Five tiles total at 200px minmax width.
  kpiItems.push(new SceneCSSGridItem({ body: switchPoeBudgetStat(serial) }));

  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
    // Drilldowns inherit their `$org` value via the var-org query param,
    // but the scene must *declare* the variable for the URL value to
    // hydrate — without this the panels ship `orgId: '$org'` literally and
    // Meraki requests short-circuit with "orgId is required".
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
          height: 160,
          body: new SceneCSSGridLayout({
            templateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
            autoRows: '140px',
            rowGap: 1,
            columnGap: 1,
            children: kpiItems,
          }),
        }),
        // VLAN distribution donut below the KPI row. Scoped to this switch
        // via the serial — the org-wide default aggregates every switch in
        // the estate, which isn't what an operator on the detail page wants.
        new SceneFlexItem({
          minHeight: 320,
          body: switchVlanDistributionDonut(serial),
        }),
        // v0.8 — stack membership + L3 SVIs side-by-side. Both panels
        // always render (memory `feedback_optional_feature_fallback`); the
        // empty-state text carries the semantic for L2 / non-stacked.
        new SceneFlexLayout({
          direction: 'row',
          children: [
            new SceneFlexItem({
              width: '50%',
              minHeight: 220,
              body: switchStackMembersTable(serial),
            }),
            new SceneFlexItem({
              width: '50%',
              minHeight: 220,
              body: switchL3InterfacesTable(serial),
            }),
          ],
        }),
        // DHCP-seen full width — rogue detection surfaces prominently.
        new SceneFlexItem({
          minHeight: 240,
          body: dhcpSeenServersTable(serial),
        }),
      ],
    }),
  });
}
