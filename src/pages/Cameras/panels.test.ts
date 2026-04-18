import { SceneQueryRunner, VizPanel } from '@grafana/scenes';
import {
  cameraEntrancesTimeseries,
  cameraInventoryTable,
  cameraLiveOccupancyTable,
  cameraOnboardingTable,
  cameraOverviewKpiRow,
  cameraRetentionProfilesPanel,
  cameraStatusKpiRow,
  cameraZoneHistoryTimeseries,
  cameraZonesTable,
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
  // setData may produce either a raw runner or a transformer wrapping one;
  // normalise via the $data chain.
  const data = panel.state.$data as { state: { $data?: unknown; queries?: unknown } } | undefined;
  if (!data) {
    throw new Error('panel has no $data');
  }
  // If it's a transformer, unwrap to the runner.
  const inner = (data as any).state.$data ?? data;
  const runner = inner as SceneQueryRunner;
  const queries = runner.state.queries as AnyQuery[];
  return queries[0];
}

describe('Cameras panels', () => {
  it('cameraStatusKpiRow yields three stat panels bound to DeviceAvailabilities filtered to camera', () => {
    const panels = cameraStatusKpiRow();
    expect(panels).toHaveLength(3);
    panels.forEach((panel) => {
      expect(panel).toBeInstanceOf(VizPanel);
      expect(panel.state.pluginId).toBe('stat');
      const q = firstQuery(panel);
      expect(q.kind).toBe(QueryKind.DeviceAvailabilities);
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

  it('cameraEntrancesTimeseries threads the $objectType variable via metrics[0]', () => {
    const panel = cameraEntrancesTimeseries('Q2MV-AAAA-BBBB');
    expect(panel.state.pluginId).toBe('timeseries');
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.CameraAnalyticsOverview);
    expect(q.serials).toEqual(['Q2MV-AAAA-BBBB']);
    expect(q.metrics).toEqual(['$objectType']);
  });

  it('cameraLiveOccupancyTable queries the live analytics kind for one serial', () => {
    const panel = cameraLiveOccupancyTable('Q2MV-AAAA-BBBB');
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.CameraAnalyticsLive);
    expect(q.serials).toEqual(['Q2MV-AAAA-BBBB']);
  });

  it('cameraZonesTable queries the zones kind for one serial', () => {
    const panel = cameraZonesTable('Q2MV-AAAA-BBBB');
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.CameraAnalyticsZones);
    expect(q.serials).toEqual(['Q2MV-AAAA-BBBB']);
  });

  it('cameraZoneHistoryTimeseries uses metrics[0]=$zone, metrics[1]=$objectType', () => {
    const panel = cameraZoneHistoryTimeseries('Q2MV-AAAA-BBBB');
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.CameraAnalyticsZoneHistory);
    expect(q.serials).toEqual(['Q2MV-AAAA-BBBB']);
    expect(q.metrics).toEqual(['$zone', '$objectType']);
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
