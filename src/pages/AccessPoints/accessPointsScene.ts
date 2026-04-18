import {
  EmbeddedScene,
  SceneCSSGridItem,
  SceneCSSGridLayout,
  SceneControlsSpacer,
  SceneFlexItem,
  SceneFlexLayout,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
  SceneVariableSet,
  VariableValueSelectors,
} from '@grafana/scenes';
import { orgVariable } from '../../scene-helpers/variables';
import { configGuardFlexItem } from '../../scene-helpers/ConfigGuard';
import {
  apClientsByDeviceTable,
  apInventoryTable,
  apStatusKpiRow,
  bandUsageSplitDonut,
  clientLatencyStatsTimeseries,
  failedConnectionRateTimeseries,
  networkChannelUtilTimeseries,
  perApRadioStatusTable,
  perSsidClientCountTimeseries,
  ssidUsageStackedTimeseries,
  wirelessApCpuLoadTimeseries,
  wirelessEthernetStatusTable,
  wirelessPacketLossByNetworkTable,
} from './panels';
import { apVariable, networkVariableForProductTypes } from './variables';

/**
 * Access Points overview — a dashboard-style view that mirrors the layout of
 * the Sensors scene:
 *   1. Config-not-set banner (collapses to zero height when configured).
 *   2. Three KPI tiles: online / alerting / offline counts for MR devices.
 *   3. Channel-utilisation timeseries spanning the selected AP(s).
 *   4. Stacked SSID usage timeseries per selected network.
 *   5. Full MR inventory table; the serial column drills into the per-AP
 *      detail page via a URL override on the shared `oneQuery` runner.
 *
 * `$ap` is included in the variable bar as a convenience filter for the
 * channel-util panel; the inventory table ignores it so users can drill in
 * without losing their filter state.
 */
export function accessPointsScene(): EmbeddedScene {
  const kpis = apStatusKpiRow().map((panel) => new SceneCSSGridItem({ body: panel }));

  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [
        orgVariable(),
        networkVariableForProductTypes(['wireless']),
        apVariable(),
      ],
    }),
    controls: [
      new VariableValueSelectors({}),
      new SceneControlsSpacer(),
      new SceneTimePicker({ isOnCanvas: true }),
      new SceneRefreshPicker({ intervals: ['30s', '1m', '5m'], isOnCanvas: true }),
    ],
    body: new SceneFlexLayout({
      direction: 'column',
      children: [
        configGuardFlexItem(),
        new SceneFlexItem({
          height: 120,
          body: new SceneCSSGridLayout({
            templateColumns: 'repeat(auto-fit, minmax(180px, 1fr))',
            autoRows: '100px',
            rowGap: 1,
            columnGap: 1,
            children: kpis,
          }),
        }),
        new SceneFlexItem({
          height: 320,
          body: networkChannelUtilTimeseries(),
        }),
        // v0.5 §4.4.3-1a — per-network client count timeseries + band-split donut.
        new SceneFlexLayout({
          direction: 'row',
          children: [
            new SceneFlexItem({ minWidth: 400, height: 320, body: perSsidClientCountTimeseries() }),
            new SceneFlexItem({ minWidth: 300, height: 320, body: bandUsageSplitDonut() }),
          ],
        }),
        // v0.5 §4.4.3-1a — failed-connection aggregation + per-network latency stats.
        new SceneFlexLayout({
          direction: 'row',
          children: [
            new SceneFlexItem({ minWidth: 400, height: 320, body: failedConnectionRateTimeseries() }),
            new SceneFlexItem({ minWidth: 400, height: 320, body: clientLatencyStatsTimeseries() }),
          ],
        }),
        // v0.5 §4.4.3-1a — org-wide AP radio-band status snapshot.
        new SceneFlexItem({
          height: 320,
          body: perApRadioStatusTable(),
        }),
        new SceneFlexItem({
          height: 320,
          body: ssidUsageStackedTimeseries(),
        }),
        new SceneFlexItem({
          minHeight: 320,
          body: apClientsByDeviceTable(),
        }),
        new SceneFlexItem({
          height: 320,
          body: wirelessPacketLossByNetworkTable(),
        }),
        new SceneFlexItem({
          height: 320,
          body: wirelessEthernetStatusTable(),
        }),
        new SceneFlexItem({
          height: 320,
          body: wirelessApCpuLoadTimeseries(),
        }),
        new SceneFlexItem({
          minHeight: 420,
          body: apInventoryTable(),
        }),
      ],
    }),
  });
}
