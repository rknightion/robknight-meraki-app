import { VizPanel } from '@grafana/scenes';
import {
  clientsPerSwitchBarChart,
  dhcpSeenServersTable,
  fleetPoeHistoryTimeseries,
  portDetailKpiStats,
  portDetailNeighborPanel,
  switchAlertsTable,
  switchL3InterfacesTable,
  switchMacAddressTable,
  switchNeighborsTable,
  switchPoeBudgetStat,
  switchPortErrorsSnapshot,
  switchStackMembersTable,
  switchStpTopologyTable,
  switchVlanDistributionDonut,
} from './panels';

// §4.4.3-1b — Jest unit tests for the five MS panels shipped in this phase.
// Shape-only assertions (viz id, title, unit where meaningful). Deeper
// render checks live in the Playwright suite which exercises real queries.

describe('switchPoeBudgetStat', () => {
  it('returns a stat VizPanel with the watt unit', () => {
    const panel = switchPoeBudgetStat('Q2SW-ABCD-0001');
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('stat');
    expect(panel.state.title).toBe('PoE draw');
    // Unit is baked onto fieldConfig.defaults via setUnit — assert so a
    // future refactor can't silently drop the watt formatting.
    expect(panel.state.fieldConfig.defaults.unit).toBe('watt');
  });
});

describe('switchPortErrorsSnapshot', () => {
  it('returns a table VizPanel titled "Port errors (snapshot)"', () => {
    const panel = switchPortErrorsSnapshot('Q2SW-ABCD-0001', '1');
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('table');
    expect(panel.state.title).toBe('Port errors (snapshot)');
  });
});

describe('switchStpTopologyTable', () => {
  it('returns a table VizPanel titled "STP topology"', () => {
    const panel = switchStpTopologyTable('N_1234');
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('table');
    expect(panel.state.title).toBe('STP topology');
  });
});

describe('switchMacAddressTable', () => {
  it('returns a table VizPanel titled "MAC address table"', () => {
    const panel = switchMacAddressTable('Q2SW-ABCD-0001');
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('table');
    expect(panel.state.title).toBe('MAC address table');
  });
});

describe('switchVlanDistributionDonut', () => {
  it('returns a piechart VizPanel titled "VLAN distribution"', () => {
    const panel = switchVlanDistributionDonut();
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('piechart');
    expect(panel.state.title).toBe('VLAN distribution');
  });
});

// v0.8 — shape assertions for the new panels. Asserting title + pluginId is
// enough to catch regressions where a refactor accidentally changes the
// viz plugin or widget. Deeper render behaviour is exercised by the
// Playwright suite against live data.

describe('fleetPoeHistoryTimeseries', () => {
  it('returns a timeseries VizPanel with the watt unit', () => {
    const panel = fleetPoeHistoryTimeseries();
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('timeseries');
    expect(panel.state.title).toBe('Fleet PoE draw');
    expect(panel.state.fieldConfig.defaults.unit).toBe('watt');
  });
});

describe('clientsPerSwitchBarChart', () => {
  it('returns a table VizPanel titled "Clients per switch"', () => {
    const panel = clientsPerSwitchBarChart();
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('table');
    expect(panel.state.title).toBe('Clients per switch');
  });
});

describe('switchNeighborsTable', () => {
  it('returns a table VizPanel titled "Neighbors (LLDP / CDP)"', () => {
    const panel = switchNeighborsTable('Q2SW-ABCD-0001');
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('table');
    expect(panel.state.title).toBe('Neighbors (LLDP / CDP)');
  });
});

describe('switchAlertsTable', () => {
  it('returns a table VizPanel titled "Alerts"', () => {
    const panel = switchAlertsTable('Q2SW-ABCD-0001');
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('table');
    expect(panel.state.title).toBe('Alerts');
  });
});

describe('dhcpSeenServersTable', () => {
  it('returns a table VizPanel for DHCP rogue detection', () => {
    const panel = dhcpSeenServersTable('Q2SW-ABCD-0001');
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('table');
    expect(panel.state.title).toContain('DHCP servers');
  });
});

describe('switchStackMembersTable', () => {
  it('returns a table VizPanel titled "Stack"', () => {
    const panel = switchStackMembersTable('Q2SW-ABCD-0001');
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('table');
    expect(panel.state.title).toBe('Stack');
  });
});

describe('switchL3InterfacesTable', () => {
  it('returns a table VizPanel titled "L3 interfaces"', () => {
    const panel = switchL3InterfacesTable('Q2SW-ABCD-0001');
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('table');
    expect(panel.state.title).toBe('L3 interfaces');
  });
});

describe('portDetailKpiStats', () => {
  it('returns a stat VizPanel titled "Port state"', () => {
    const panel = portDetailKpiStats('Q2SW-ABCD-0001', '1');
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('stat');
    expect(panel.state.title).toBe('Port state');
  });
});

describe('portDetailNeighborPanel', () => {
  it('returns a table VizPanel titled "Neighbor (LLDP / CDP)"', () => {
    const panel = portDetailNeighborPanel('Q2SW-ABCD-0001', '1');
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('table');
    expect(panel.state.title).toBe('Neighbor (LLDP / CDP)');
  });
});
