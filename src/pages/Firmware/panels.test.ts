import {
  deviceEolTable,
  firmwareEolSoonCountStat,
  firmwarePendingCountStat,
  firmwarePendingTable,
  firmwareScheduledCountStat,
} from './panels';

describe('Firmware panels', () => {
  it('firmwareScheduledCountStat returns a stat panel', () => {
    const panel = firmwareScheduledCountStat();
    expect(panel).toBeDefined();
    expect(panel.state.pluginId).toBe('stat');
  });

  it('firmwarePendingCountStat returns a stat panel', () => {
    const panel = firmwarePendingCountStat();
    expect(panel).toBeDefined();
    expect(panel.state.pluginId).toBe('stat');
  });

  it('firmwareEolSoonCountStat returns a stat panel', () => {
    const panel = firmwareEolSoonCountStat();
    expect(panel).toBeDefined();
    expect(panel.state.pluginId).toBe('stat');
  });

  it('firmwarePendingTable returns a table panel', () => {
    const panel = firmwarePendingTable();
    expect(panel).toBeDefined();
    expect(panel.state.pluginId).toBe('table');
  });

  it('deviceEolTable returns a table panel', () => {
    const panel = deviceEolTable();
    expect(panel).toBeDefined();
    expect(panel.state.pluginId).toBe('table');
  });
});
