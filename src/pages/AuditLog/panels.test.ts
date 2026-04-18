import { auditLogTable, auditLogTimelineBarChart } from './panels';

describe('AuditLog panels', () => {
  it('auditLogTable factory returns a defined panel', () => {
    const panel = auditLogTable();
    expect(panel).toBeDefined();
    expect(panel.state.pluginId).toBe('table');
  });

  it('auditLogTimelineBarChart factory returns a defined panel', () => {
    const panel = auditLogTimelineBarChart();
    expect(panel).toBeDefined();
    expect(panel.state.pluginId).toBe('timeseries');
  });
});
