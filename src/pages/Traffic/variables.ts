import { CustomVariable, QueryVariable, SceneVariableSet } from '@grafana/scenes';
import { networkVariable, orgVariable } from '../../scene-helpers/variables';

/**
 * Variable set for the Traffic page:
 *
 *  - `$org`         — the organisation picker (single-select, hydrated from
 *                     /metricFind:Organizations).
 *  - `$network`     — multi-select network picker; the L7 panels fan out
 *                     across selected networks. Defaults to "All".
 *  - `$deviceType`  — optional /networks/{id}/traffic deviceType filter.
 *                     Stays on the scene rather than the panels because the
 *                     filter applies uniformly to every per-network panel.
 *
 * Network picker is filtered by productTypes elsewhere on family-specific
 * pages (e.g. the Switches page only shows switch-bearing networks). The
 * Traffic page is family-agnostic — every product type carries traffic
 * analysis — so we use the un-filtered `networkVariable()` factory.
 */
export function trafficVariables(): SceneVariableSet {
  return new SceneVariableSet({
    variables: [orgVariable(), networkVariable(), deviceTypeVariable()],
  });
}

/**
 * `$deviceType` — static filter for the per-network /networks/{id}/traffic
 * call. Allowed values per the Meraki spec: "combined" (default), "wireless",
 * "switch", "appliance".
 *
 * The `All` sentinel is wired to the literal `combined` value (rather than an
 * empty allValue) because the Meraki endpoint defaults `deviceType` to
 * `combined` when absent — keeping the URL deterministic avoids a cache key
 * split between the two equivalent forms.
 */
export function deviceTypeVariable(): CustomVariable {
  return new CustomVariable({
    name: 'deviceType',
    label: 'Device type',
    query: 'combined,wireless,switch,appliance',
    value: 'combined',
    text: 'Combined',
    isMulti: false,
  });
}

/**
 * Re-exported network factory so scene files can import from a single place.
 * Saves callers from juggling two import sources just to set up the scene.
 */
export function trafficNetworkVariable(): QueryVariable {
  return networkVariable();
}
