import { SceneQueryRunner } from '@grafana/scenes';
import { QueryKind } from '../../datasource/types';
import { MERAKI_DS_UID } from '../../scene-helpers/datasource';
import {
  bandUsageSplitDonut,
  clientLatencyStatsTimeseries,
  failedConnectionRateTimeseries,
  perApRadioStatusTable,
  perSsidClientCountTimeseries,
} from './panels';

// v0.5 §4.4.3-1a panel-factory smoke tests. Each factory must emit a
// VizPanel whose data-provider is a SceneQueryRunner pointed at the
// Meraki DS (uid = MERAKI_DS_UID) and carrying exactly one query for
// the expected QueryKind.

/** Walk the scene-object's $data tree to find the first SceneQueryRunner. */
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

describe('AccessPoints v0.5 §4.4.3-1a panel factories', () => {
  it('perSsidClientCountTimeseries wires wirelessClientCountHistory', () => {
    const panel = perSsidClientCountTimeseries();
    const runner = findQueryRunner(panel);
    expect(runner).not.toBeNull();
    expect(runner!.state.datasource?.uid).toBe(MERAKI_DS_UID);
    const q = runner!.state.queries[0] as unknown as { kind: QueryKind };
    expect(q.kind).toBe(QueryKind.WirelessClientCountHistory);
  });

  it('bandUsageSplitDonut reshapes the wirelessUsage kind', () => {
    const panel = bandUsageSplitDonut();
    const runner = findQueryRunner(panel);
    expect(runner).not.toBeNull();
    expect(runner!.state.datasource?.uid).toBe(MERAKI_DS_UID);
    const q = runner!.state.queries[0] as unknown as { kind: QueryKind };
    expect(q.kind).toBe(QueryKind.WirelessUsage);
  });

  it('perApRadioStatusTable wires deviceRadioStatus', () => {
    const panel = perApRadioStatusTable();
    const runner = findQueryRunner(panel);
    expect(runner).not.toBeNull();
    expect(runner!.state.datasource?.uid).toBe(MERAKI_DS_UID);
    const q = runner!.state.queries[0] as unknown as { kind: QueryKind };
    expect(q.kind).toBe(QueryKind.DeviceRadioStatus);
  });

  it('failedConnectionRateTimeseries wires wirelessFailedConnections', () => {
    const panel = failedConnectionRateTimeseries();
    const runner = findQueryRunner(panel);
    expect(runner).not.toBeNull();
    expect(runner!.state.datasource?.uid).toBe(MERAKI_DS_UID);
    const q = runner!.state.queries[0] as unknown as { kind: QueryKind };
    expect(q.kind).toBe(QueryKind.WirelessFailedConnections);
  });

  it('clientLatencyStatsTimeseries wires wirelessLatencyStats', () => {
    const panel = clientLatencyStatsTimeseries();
    const runner = findQueryRunner(panel);
    expect(runner).not.toBeNull();
    expect(runner!.state.datasource?.uid).toBe(MERAKI_DS_UID);
    const q = runner!.state.queries[0] as unknown as { kind: QueryKind };
    expect(q.kind).toBe(QueryKind.WirelessLatencyStats);
  });
});
