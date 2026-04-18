import { CustomVariable, QueryVariable } from '@grafana/scenes';
import { VariableRefresh } from '@grafana/schema';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { deviceVariable } from '../../scene-helpers/variables';
import { QueryKind } from '../../datasource/types';

/**
 * `$camera` — single-select MV camera picker hydrated from the Meraki Devices
 * metricFind handler filtered to `productTypes=['camera']`. Backed by the
 * shared {@link deviceVariable} factory so the cascade semantics match the
 * MR/MS/MX/MG pickers: cascades from `$org`, single-select (per-device
 * endpoints accept one serial), default to the "All" sentinel.
 */
export function cameraVariable(): QueryVariable {
  return deviceVariable({ name: 'camera', label: 'Camera', productType: 'camera' });
}

/**
 * `$objectType` — static object-type filter for camera analytics queries.
 * The Meraki MV analytics endpoints accept `person` or `vehicle` and return
 * per-object-type entrance counts. We keep the vocabulary local (no API
 * roundtrip) because the set is hard-coded upstream; the default of `person`
 * matches the Meraki API's default and is the more common use case.
 */
export function cameraObjectTypeVariable(): CustomVariable {
  return new CustomVariable({
    name: 'objectType',
    label: 'Object type',
    query: 'person,vehicle',
    value: 'person',
    text: 'person',
    includeAll: false,
    isMulti: false,
  });
}

/**
 * `$zone` — per-camera zone picker hydrated from the `CameraAnalyticsZones`
 * metricFind kind. Depends on `$camera`: when the user changes camera, the
 * zone list re-hydrates because `serials: ['$camera']` carries through the
 * variable query. Single-select with an `All : ''` sentinel so panels can
 * fall back to an unfiltered view.
 *
 * The backend emits `{text: "<type>: <label>", value: "<zoneId>"}` tuples —
 * see `pkg/plugin/query/metricfind.go::runMetricFind` under the
 * `KindCameraAnalyticsZones` branch.
 */
export function cameraZoneVariable(): QueryVariable {
  return new QueryVariable({
    name: 'zone',
    label: 'Zone',
    datasource: MERAKI_DS_REF,
    query: {
      kind: QueryKind.CameraAnalyticsZones,
      refId: 'zones',
      orgId: '$org',
      serials: ['$camera'],
    },
    includeAll: true,
    defaultToAll: true,
    allValue: '',
    isMulti: false,
    refresh: VariableRefresh.onDashboardLoad,
    sort: 1,
  });
}
