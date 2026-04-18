import {
  clientLatencyTrend,
  clientSearchTable,
  newClientsTable,
  sessionHistoryTimeseries,
  topTalkersTable,
} from './panels';

describe('Clients panels', () => {
  it('topTalkersTable factory returns a table panel', () => {
    const panel = topTalkersTable();
    expect(panel).toBeDefined();
    expect(panel.state.pluginId).toBe('table');
  });

  it('newClientsTable factory returns a table panel', () => {
    const panel = newClientsTable();
    expect(panel).toBeDefined();
    expect(panel.state.pluginId).toBe('table');
  });

  it('clientSearchTable factory returns a table panel', () => {
    const panel = clientSearchTable();
    expect(panel).toBeDefined();
    expect(panel.state.pluginId).toBe('table');
  });

  it('sessionHistoryTimeseries factory returns a timeseries panel', () => {
    const panel = sessionHistoryTimeseries();
    expect(panel).toBeDefined();
    expect(panel.state.pluginId).toBe('timeseries');
  });

  it('clientLatencyTrend factory returns a stat panel', () => {
    const panel = clientLatencyTrend();
    expect(panel).toBeDefined();
    expect(panel.state.pluginId).toBe('stat');
  });
});
