import { CustomVariable, QueryVariable } from '@grafana/scenes';
import { VariableRefresh } from '@grafana/schema';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';

/**
 * $ap — single-select AP picker hydrated from the Meraki Devices metricFind
 * handler with `productTypes=['wireless']`. Used on the overview scene so
 * users can pin a panel to one access point without cascading through the
 * drilldown.
 *
 * Kept single-select to match the Meraki API's per-serial endpoints;
 * multi-select would force panels to fan out into N frames per series and
 * break the legend contract.
 */
export function apVariable(): QueryVariable {
  return new QueryVariable({
    name: 'ap',
    label: 'Access point',
    datasource: MERAKI_DS_REF,
    query: {
      kind: QueryKind.Devices,
      refId: 'aps',
      orgId: '$org',
      productTypes: ['wireless'],
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
 * $band — static wireless band filter. `CustomVariable` is served without an
 * HTTP roundtrip; the three Wi-Fi bands are a stable vocabulary and we don't
 * need to paginate them. The RF scene uses this to scope the per-band panel
 * overrides.
 */
export function wirelessBandVariable(): CustomVariable {
  return new CustomVariable({
    name: 'band',
    label: 'Band',
    query: 'All : ,2.4,5,6',
    value: '',
    text: 'All',
    includeAll: true,
    defaultToAll: true,
    allValue: '',
    isMulti: false,
  });
}

/**
 * $network — cascading variable that depends on $org, filtered by product
 * type so MR / MS / MT scenes only show the networks that matter. Mirror of
 * `networkVariable()` in `src/scene-helpers/variables.ts`; factored here so
 * the shared helper stays untouched during Wave 3.
 */
export function networkVariableForProductTypes(productTypes: string[]): QueryVariable {
  return new QueryVariable({
    name: 'network',
    label: 'Network',
    datasource: MERAKI_DS_REF,
    query: {
      kind: QueryKind.Networks,
      refId: 'networks',
      orgId: '$org',
      productTypes,
    },
    includeAll: true,
    defaultToAll: true,
    isMulti: true,
    allValue: '',
    refresh: VariableRefresh.onDashboardLoad,
    sort: 1,
  });
}
