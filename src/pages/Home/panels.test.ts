import { SceneQueryRunner } from '@grafana/scenes';
import { QueryKind } from '../../datasource/types';
import { MERAKI_DS_UID } from '../../scene-helpers/datasource';
import {
  HOME_AT_A_GLANCE_KPIS,
  availabilityByFamilyStackedBar,
  homeAtAGlanceStats,
  orgChangeFeedTile,
  orgHealthStat,
} from './panels';

// v0.5 §4.4.5 Home-panel factory smoke tests. These assert the wiring of the
// reworked Home layout: every KPI tile must be bound to the orgHealthSummary
// kind, the change-feed tile to orgChangeFeed, and the by-family bar to the
// new deviceStatusByFamily kind.

/** Walk the scene-object $data tree to find the first SceneQueryRunner. */
function findQueryRunner(panel: any): SceneQueryRunner | null {
  let data = panel.state?.$data;
  while (data) {
    if (data instanceof SceneQueryRunner) {
      return data;
    }
    data = data.state?.$data;
  }
  return null;
}

describe('Home v0.5 §4.4.5 panel factories', () => {
  it('exposes exactly six at-a-glance KPI specs', () => {
    expect(HOME_AT_A_GLANCE_KPIS).toHaveLength(6);
    const fields = HOME_AT_A_GLANCE_KPIS.map((s) => s.field);
    expect(fields).toEqual([
      'devicesOffline',
      'alertsCritical',
      'licensesExp30d',
      'firmwareDrift',
      'apiErrorPct',
      'uplinksDown',
    ]);
  });

  it('every KPI spec carries at least base + amber/red thresholds', () => {
    for (const spec of HOME_AT_A_GLANCE_KPIS) {
      expect(spec.thresholds.length).toBeGreaterThanOrEqual(2);
      // First step is the base colour; subsequent steps must be strictly ascending.
      for (let i = 1; i < spec.thresholds.length; i++) {
        expect(spec.thresholds[i].value).toBeGreaterThan(spec.thresholds[i - 1].value);
      }
    }
  });

  it('orgHealthStat binds to the orgHealthSummary kind on the Meraki DS', () => {
    const panel = orgHealthStat(HOME_AT_A_GLANCE_KPIS[0]);
    const runner = findQueryRunner(panel);
    expect(runner).not.toBeNull();
    expect(runner!.state.datasource?.uid).toBe(MERAKI_DS_UID);
    const q = runner!.state.queries[0] as unknown as { kind: QueryKind; orgId: string };
    expect(q.kind).toBe(QueryKind.OrgHealthSummary);
    expect(q.orgId).toBe('$org');
  });

  it('homeAtAGlanceStats emits one VizPanel per KPI spec', () => {
    const panels = homeAtAGlanceStats();
    expect(panels).toHaveLength(HOME_AT_A_GLANCE_KPIS.length);
    for (const panel of panels) {
      const runner = findQueryRunner(panel);
      expect(runner).not.toBeNull();
      const q = runner!.state.queries[0] as unknown as { kind: QueryKind };
      expect(q.kind).toBe(QueryKind.OrgHealthSummary);
    }
  });

  it('availabilityByFamilyStackedBar wires the deviceStatusByFamily kind', () => {
    const panel = availabilityByFamilyStackedBar();
    const runner = findQueryRunner(panel);
    expect(runner).not.toBeNull();
    expect(runner!.state.datasource?.uid).toBe(MERAKI_DS_UID);
    const q = runner!.state.queries[0] as unknown as { kind: QueryKind };
    expect(q.kind).toBe(QueryKind.DeviceStatusByFamily);
  });

  it('orgChangeFeedTile wires the orgChangeFeed kind', () => {
    const panel = orgChangeFeedTile();
    const runner = findQueryRunner(panel);
    expect(runner).not.toBeNull();
    const q = runner!.state.queries[0] as unknown as { kind: QueryKind; orgId: string };
    expect(q.kind).toBe(QueryKind.OrgChangeFeed);
    expect(q.orgId).toBe('$org');
  });
});
