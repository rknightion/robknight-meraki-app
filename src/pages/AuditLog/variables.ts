import { CustomVariable } from '@grafana/scenes';

/**
 * `$admin` — free-form admin-id filter for the Audit Log scene.
 *
 * Why a CustomVariable rather than a QueryVariable: `metricFindQuery`
 * returns arbitrary `{text, value}` tuples, not admin IDs, and the
 * backend exposes admin ids only inline on the change log frame itself.
 * A permissive text input keeps the surface area tiny — the operator
 * pastes the admin id (or email) they're hunting for and the query
 * forwards it as the `metrics[0]` filter; the backend treats the empty
 * string as "no filter".
 *
 * `includeAll: true` with `allValue: ''` means the default view ships
 * every admin — matches the rest of the scene's variables.
 */
export function adminVariable(): CustomVariable {
  return new CustomVariable({
    name: 'admin',
    label: 'Admin',
    query: '',
    value: '',
    text: '',
    includeAll: true,
    allValue: '',
    isMulti: false,
  });
}
