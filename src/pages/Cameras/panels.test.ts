import { SceneQueryRunner, VizPanel } from '@grafana/scenes';
import {
  cameraBoundariesTable,
  cameraDetectionsTimeseries,
  cameraInventoryTable,
  cameraOnboardingTable,
  cameraOverviewKpiRow,
  cameraRetentionProfilesPanel,
  cameraStatusKpiRow,
} from './panels';
import { QueryKind } from '../../datasource/types';

type AnyQuery = {
  refId: string;
  kind?: QueryKind;
  orgId?: string;
  serials?: string[];
  metrics?: string[];
  productTypes?: string[];
  networkIds?: string[];
};

function firstQuery(panel: VizPanel): AnyQuery {
  const data = panel.state.$data as { state: { $data?: unknown; queries?: unknown } } | undefined;
  if (!data) {
    throw new Error('panel has no $data');
  }
  const inner = (data as any).state.$data ?? data;
  const runner = inner as SceneQueryRunner;
  const queries = runner.state.queries as AnyQuery[];
  return queries[0];
}

function allQueries(panel: VizPanel): AnyQuery[] {
  const data = panel.state.$data as { state: { $data?: unknown; queries?: unknown } } | undefined;
  if (!data) {
    throw new Error('panel has no $data');
  }
  const inner = (data as any).state.$data ?? data;
  const runner = inner as SceneQueryRunner;
  return runner.state.queries as AnyQuery[];
}

describe('Cameras panels', () => {
  it('cameraStatusKpiRow yields three stat panels bound to the server-side availability count aggregator', () => {
    const panels = cameraStatusKpiRow();
    expect(panels).toHaveLength(3);
    panels.forEach((panel) => {
      expect(panel).toBeInstanceOf(VizPanel);
      expect(panel.state.pluginId).toBe('stat');
      const q = firstQuery(panel);
      expect(q.kind).toBe(QueryKind.DeviceAvailabilityCounts);
      expect(q.productTypes).toEqual(['camera']);
    });
  });

  it('cameraOnboardingTable targets the CameraOnboarding kind', () => {
    const panel = cameraOnboardingTable();
    expect(panel.state.pluginId).toBe('table');
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.CameraOnboarding);
  });

  it('cameraInventoryTable filters Devices to productType=camera', () => {
    const panel = cameraInventoryTable();
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.Devices);
    expect(q.productTypes).toEqual(['camera']);
  });

  it('cameraDetectionsTimeseries threads the $objectType variable via metrics[1]', () => {
    const panel = cameraDetectionsTimeseries('Q2MV-AAAA-BBBB');
    expect(panel.state.pluginId).toBe('timeseries');
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.CameraDetectionsHistory);
    expect(q.serials).toEqual(['Q2MV-AAAA-BBBB']);
    expect(q.metrics).toEqual(['', '$objectType']);
  });

  it('cameraBoundariesTable runs both area + line queries so the merged frame carries both kinds', () => {
    const panel = cameraBoundariesTable('Q2MV-AAAA-BBBB');
    const queries = allQueries(panel);
    expect(queries).toHaveLength(2);
    const kinds = queries.map((q) => q.kind).sort();
    expect(kinds).toEqual([QueryKind.CameraBoundaryAreas, QueryKind.CameraBoundaryLines].sort());
    queries.forEach((q) => {
      expect(q.serials).toEqual(['Q2MV-AAAA-BBBB']);
    });
  });

  it('cameraRetentionProfilesPanel binds to the $network variable', () => {
    const panel = cameraRetentionProfilesPanel();
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.CameraRetentionProfiles);
    expect(q.networkIds).toEqual(['$network']);
  });

  it('cameraOverviewKpiRow yields four stat panels including a Status tile', () => {
    const panels = cameraOverviewKpiRow('Q2MV-AAAA-BBBB');
    expect(panels).toHaveLength(4);
    panels.forEach((panel) => {
      expect(panel.state.pluginId).toBe('stat');
    });
    // First panel is the Status tile; its underlying query should target
    // DeviceAvailabilities. Subsequent tiles read from Devices.
    expect(firstQuery(panels[0]).kind).toBe(QueryKind.DeviceAvailabilities);
    expect(firstQuery(panels[1]).kind).toBe(QueryKind.Devices);
  });
});
