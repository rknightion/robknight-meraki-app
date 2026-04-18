import { SceneQueryRunner, VizPanel } from '@grafana/scenes';
import {
  mgConnectivityPanel,
  mgInventoryTable,
  mgLanPanel,
  mgOverviewKpiRow,
  mgPortForwardingTable,
  mgSignalBarChart,
  mgSignalGauge,
  mgStatusKpiRow,
  mgUplinkFleetTable,
  mgUplinkTable,
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

describe('Cellular Gateways panels', () => {
  it('mgStatusKpiRow yields three stat panels bound to DeviceAvailabilities filtered to cellularGateway', () => {
    const panels = mgStatusKpiRow();
    expect(panels).toHaveLength(3);
    panels.forEach((panel) => {
      expect(panel.state.pluginId).toBe('stat');
      const q = firstQuery(panel);
      expect(q.kind).toBe(QueryKind.DeviceAvailabilities);
      expect(q.productTypes).toEqual(['cellularGateway']);
    });
  });

  it('mgInventoryTable filters Devices to productType=cellularGateway', () => {
    const panel = mgInventoryTable();
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.Devices);
    expect(q.productTypes).toEqual(['cellularGateway']);
  });

  it('mgUplinkFleetTable queries the MgUplinks kind without a serial filter', () => {
    const panel = mgUplinkFleetTable();
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.MgUplinks);
    expect(q.serials).toBeUndefined();
  });

  it('mgSignalBarChart is a bar chart backed by MgUplinks', () => {
    const panel = mgSignalBarChart();
    expect(panel.state.pluginId).toBe('barchart');
    expect(firstQuery(panel).kind).toBe(QueryKind.MgUplinks);
  });

  it('mgUplinkTable filters MgUplinks to one serial', () => {
    const panel = mgUplinkTable('Q2MG-AAAA-BBBB');
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.MgUplinks);
    expect(q.serials).toEqual(['Q2MG-AAAA-BBBB']);
  });

  it('mgSignalGauge is a gauge reading one signal column', () => {
    const panel = mgSignalGauge('Q2MG-AAAA-BBBB', 'rsrpDb');
    expect(panel.state.pluginId).toBe('gauge');
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.MgUplinks);
    expect(q.serials).toEqual(['Q2MG-AAAA-BBBB']);
  });

  it('mgPortForwardingTable queries MgPortForwarding scoped to one serial', () => {
    const panel = mgPortForwardingTable('Q2MG-AAAA-BBBB');
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.MgPortForwarding);
    expect(q.serials).toEqual(['Q2MG-AAAA-BBBB']);
  });

  it('mgLanPanel queries MgLan scoped to one serial', () => {
    const panel = mgLanPanel('Q2MG-AAAA-BBBB');
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.MgLan);
    expect(q.serials).toEqual(['Q2MG-AAAA-BBBB']);
  });

  it('mgConnectivityPanel queries MgConnectivity for one network', () => {
    const panel = mgConnectivityPanel('N_123');
    const q = firstQuery(panel);
    expect(q.kind).toBe(QueryKind.MgConnectivity);
    expect(q.networkIds).toEqual(['N_123']);
  });

  it('mgOverviewKpiRow yields four stat panels including the Status tile', () => {
    const panels = mgOverviewKpiRow('Q2MG-AAAA-BBBB');
    expect(panels).toHaveLength(4);
    panels.forEach((panel) => {
      expect(panel.state.pluginId).toBe('stat');
    });
    expect(firstQuery(panels[0]).kind).toBe(QueryKind.DeviceAvailabilities);
    expect(firstQuery(panels[1]).kind).toBe(QueryKind.Devices);
  });
});
