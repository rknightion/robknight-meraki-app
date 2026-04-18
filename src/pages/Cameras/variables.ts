import { CustomVariable, QueryVariable } from '@grafana/scenes';
import { VariableRefresh } from '@grafana/schema';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { deviceVariable } from '../../scene-helpers/variables';
import { QueryKind } from '../../datasource/types';

/**
 * `$camera` — single-select MV camera picker hydrated from the Meraki Devices
 * metricFind handler filtered to `productTypes=['camera']`.
 */
export function cameraVariable(): QueryVariable {
  return deviceVariable({ name: 'camera', label: 'Camera', productType: 'camera' });
}

/**
 * `$objectType` — static object-type filter for camera detections queries.
 * The Meraki MV detections endpoints accept `person` or `vehicle` and return
 * per-object-type in/out counts. Defaults to `person` to match Meraki's
 * server-side default.
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
 * `$boundary` — per-camera boundary picker hydrated from the
 * `CameraBoundaryAreas` metricFind kind. Depends on `$camera` (or an explicit
 * `serial` parameter passed by the detail page), so the boundary list
 * re-hydrates when the camera changes.
 *
 * The backend emits `{text: "<name> (<kind>)", value: "<boundaryId>"}` tuples
 * — see `pkg/plugin/query/metricfind.go::runMetricFind` under the
 * `KindCameraBoundaryAreas` / `KindCameraBoundaryLines` branches. We bind
 * only to area boundaries here for simplicity; panels that need line
 * boundaries can pass an explicit boundaryId through `metrics[0]`.
 */
export function cameraBoundaryVariable(serial?: string): QueryVariable {
  return new QueryVariable({
    name: 'boundary',
    label: 'Boundary',
    datasource: MERAKI_DS_REF,
    query: {
      kind: QueryKind.CameraBoundaryAreas,
      refId: 'boundaries',
      orgId: '$org',
      serials: serial ? [serial] : ['$camera'],
    },
    includeAll: true,
    defaultToAll: true,
    allValue: '',
    isMulti: false,
    refresh: VariableRefresh.onDashboardLoad,
    sort: 1,
  });
}
