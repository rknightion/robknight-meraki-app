import { VizPanel } from '@grafana/scenes';
import {
  applianceUplinksOverviewRow,
  mxStatusKpiRow,
  mxUplinksUsageByNetworkTable,
  mxUplinksUsageHistoryTimeseries,
  uplinkLossLatencyHistoryTimeseries,
  uplinkLossLatencyTimeseries,
} from './panels';

describe('mxStatusKpiRow', () => {
  it('returns three VizPanels with online / alerting / offline titles', () => {
    const panels = mxStatusKpiRow();

    expect(panels).toHaveLength(3);
    // Each entry is a built VizPanel — assert both shape and order so the
    // CSSGrid layout in appliancesScene renders the tiles left-to-right as
    // Online / Alerting / Offline.
    expect(panels.every((p) => p instanceof VizPanel)).toBe(true);
    expect(panels.map((p) => p.state.title)).toEqual([
      'Appliances online',
      'Appliances alerting',
      'Appliances offline',
    ]);
  });
});

describe('applianceUplinksOverviewRow', () => {
  it('returns four VizPanels for the uplink status buckets', () => {
    const panels = applianceUplinksOverviewRow();

    expect(panels).toHaveLength(4);
    expect(panels.every((p) => p instanceof VizPanel)).toBe(true);
    expect(panels.map((p) => p.state.title)).toEqual([
      'Active',
      'Ready',
      'Failed',
      'Not connected',
    ]);
  });
});

describe('uplinkLossLatencyTimeseries', () => {
  it('sets unit=percent with min/max 0-100 for the lossPercent metric', () => {
    const panel = uplinkLossLatencyTimeseries('Q2XX-ABCD-1234', 'lossPercent');

    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('timeseries');
    // Unit + bounds are baked onto fieldConfig.defaults via PanelBuilders'
    // setUnit/setMin/setMax; assert them here to protect against a future
    // refactor that accidentally swaps the metric branches.
    expect(panel.state.fieldConfig.defaults.unit).toBe('percent');
    expect(panel.state.fieldConfig.defaults.min).toBe(0);
    expect(panel.state.fieldConfig.defaults.max).toBe(100);
  });

  it('sets unit=ms with no bounds for the latencyMs metric', () => {
    const panel = uplinkLossLatencyTimeseries('Q2XX-ABCD-1234', 'latencyMs');

    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('timeseries');
    expect(panel.state.fieldConfig.defaults.unit).toBe('ms');
    // Latency can spike into the seconds; leaving min/max unset lets the
    // y-axis autoscale instead of clipping real outages to [0, 100].
    expect(panel.state.fieldConfig.defaults.min).toBeUndefined();
    expect(panel.state.fieldConfig.defaults.max).toBeUndefined();
  });
});

describe('uplinkLossLatencyHistoryTimeseries', () => {
  it('returns a timeseries VizPanel with percent unit for lossPercent', () => {
    const panel = uplinkLossLatencyHistoryTimeseries('lossPercent');
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('timeseries');
    expect(panel.state.fieldConfig.defaults.unit).toBe('percent');
    expect(panel.state.fieldConfig.defaults.min).toBe(0);
    expect(panel.state.fieldConfig.defaults.max).toBe(100);
  });

  it('returns a timeseries VizPanel with ms unit for latencyMs', () => {
    const panel = uplinkLossLatencyHistoryTimeseries('latencyMs');
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('timeseries');
    expect(panel.state.fieldConfig.defaults.unit).toBe('ms');
    expect(panel.state.fieldConfig.defaults.min).toBeUndefined();
    expect(panel.state.fieldConfig.defaults.max).toBeUndefined();
  });
});

describe('mxUplinksUsageHistoryTimeseries', () => {
  it('returns a timeseries VizPanel with kbytes unit', () => {
    const panel = mxUplinksUsageHistoryTimeseries();
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('timeseries');
    expect(panel.state.fieldConfig.defaults.unit).toBe('kbytes');
  });
});

describe('mxUplinksUsageByNetworkTable', () => {
  it('returns a table VizPanel', () => {
    const panel = mxUplinksUsageByNetworkTable();
    expect(panel).toBeInstanceOf(VizPanel);
    expect(panel.state.pluginId).toBe('table');
  });
});
