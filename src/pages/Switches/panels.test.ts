import { VizPanel } from '@grafana/scenes';
import {
  switchMacAddressTable,
  switchPoeBudgetStat,
  switchPortErrorsSnapshot,
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
