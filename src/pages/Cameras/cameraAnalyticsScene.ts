import {
  EmbeddedScene,
  QueryVariable,
  SceneControlsSpacer,
  SceneFlexItem,
  SceneFlexLayout,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
  SceneVariableSet,
  VariableValueSelectors,
} from '@grafana/scenes';
import { VariableRefresh } from '@grafana/schema';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';
import {
  cameraEntrancesTimeseries,
  cameraLiveOccupancyTable,
  cameraZoneHistoryTimeseries,
} from './panels';
import { cameraObjectTypeVariable } from './variables';

/**
 * Per-camera zone picker — hydrated from `CameraAnalyticsZones` scoped to
 * the detail page's serial (via the URL parameter rather than a cascading
 * `$camera` variable). Kept inline here because the shared factory in
 * `variables.ts` uses `$camera` which isn't populated on the detail route.
 */
function zoneVariableForSerial(serial: string): QueryVariable {
  return new QueryVariable({
    name: 'zone',
    label: 'Zone',
    datasource: MERAKI_DS_REF,
    query: {
      kind: QueryKind.CameraAnalyticsZones,
      refId: 'zones',
      orgId: '$org',
      serials: [serial],
    },
    includeAll: true,
    defaultToAll: true,
    allValue: '',
    isMulti: false,
    refresh: VariableRefresh.onDashboardLoad,
    sort: 1,
  });
}

/**
 * Per-camera Analytics tab — entrance counts over time, a live snapshot of
 * per-zone occupancy, and a zone-history chart driven by the `$zone` picker.
 *
 * Time range defaults to 24h — the Meraki analytics endpoint aggregates
 * hour-sized buckets by default, so a longer window gives users a useful
 * view without the backend's 5m resolution fight-back.
 *
 * Variables:
 *  - `$objectType` — person / vehicle switch shared across all three panels.
 *  - `$zone` — scoped to this camera (serial baked in via the variable's
 *    `serials: [serial]`).
 */
export function cameraAnalyticsScene(serial: string): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-24h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [cameraObjectTypeVariable(), zoneVariableForSerial(serial)],
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
        new SceneFlexItem({
          height: 320,
          body: cameraEntrancesTimeseries(serial),
        }),
        new SceneFlexItem({
          minHeight: 260,
          body: cameraLiveOccupancyTable(serial),
        }),
        new SceneFlexItem({
          height: 320,
          body: cameraZoneHistoryTimeseries(serial),
        }),
      ],
    }),
  });
}
