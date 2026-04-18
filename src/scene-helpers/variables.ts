import { CustomVariable, QueryVariable, SceneVariableSet } from '@grafana/scenes';
import { VariableRefresh } from '@grafana/schema';
import { MERAKI_DS_REF } from './datasource';
import { QueryKind } from '../datasource/types';
import type { MerakiProductType } from '../types';

/**
 * Shorthand for every drilldown / detail scene that needs just `$org`
 * hydrated from the URL's `var-org` query param. Without declaring the
 * variable on the scene, per-panel queries ship `orgId: '$org'` literally
 * and the backend 400s with "orgId is required".
 */
export function orgOnlyVariables(): SceneVariableSet {
  return new SceneVariableSet({ variables: [orgVariable()] });
}

/**
 * $org — hydrated from the Meraki DS metricFindQuery. Default refreshes on dashboard load so
 * users always see the current org inventory without a hard reload.
 */
export function orgVariable(): QueryVariable {
  return new QueryVariable({
    name: 'org',
    label: 'Organization',
    datasource: MERAKI_DS_REF,
    query: { kind: QueryKind.Organizations, refId: 'orgs' },
    includeAll: false,
    refresh: VariableRefresh.onDashboardLoad,
    sort: 1,
  });
}

/**
 * $network — cascading variable that depends on $org. Multi-select is enabled so sensor scenes
 * can span sites.
 */
export function networkVariable(): QueryVariable {
  return new QueryVariable({
    name: 'network',
    label: 'Network',
    datasource: MERAKI_DS_REF,
    query: { kind: QueryKind.Networks, refId: 'networks', orgId: '$org' },
    includeAll: true,
    defaultToAll: true,
    isMulti: true,
    allValue: '',
    refresh: VariableRefresh.onDashboardLoad,
    sort: 1,
  });
}

/**
 * $network filtered by one or more productTypes. Shared factory used by every
 * per-family overview scene (MR/MS/MX/MV/MG/MT) so the dropdown only lists
 * networks that carry the relevant product — a wireless network for Access
 * Points, a switch network for Switches, etc.
 */
export function networkVariableForProductTypes(
  productTypes: MerakiProductType[]
): QueryVariable {
  return new QueryVariable({
    name: 'network',
    label: 'Network',
    datasource: MERAKI_DS_REF,
    query: { kind: QueryKind.Networks, refId: 'networks', orgId: '$org', productTypes },
    includeAll: true,
    defaultToAll: true,
    isMulti: true,
    allValue: '',
    refresh: VariableRefresh.onDashboardLoad,
    sort: 1,
  });
}

/**
 * Single-select device picker hydrated from the Meraki Devices metricFind
 * handler. Shared factory that replaces per-area copies like `apVariable()`,
 * `switchVariable()`, and the new `mxVariable()`/`cameraVariable()`/`mgVariable()`.
 *
 * Single-select by design: Meraki per-device endpoints accept one serial at a
 * time, so multi-select would force panels to fan out into N frames per series
 * and break the legend contract.
 */
export function deviceVariable(params: {
  name: string;
  label: string;
  productType: MerakiProductType;
}): QueryVariable {
  return new QueryVariable({
    name: params.name,
    label: params.label,
    datasource: MERAKI_DS_REF,
    query: {
      kind: QueryKind.Devices,
      refId: `${params.name}s`,
      orgId: '$org',
      productTypes: [params.productType],
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
 * `clientVariable` — free-form text variable used by the Clients page for
 * MAC search and per-client drilldown.
 *
 * Why a CustomVariable rather than a QueryVariable: there is no Meraki API
 * that enumerates every client across an org without already knowing one
 * (the org-wide /clients/search call requires a `mac` parameter). A
 * permissive text input keeps the surface area tiny — operators paste the
 * MAC (or partial MAC) they're hunting for, and the panel queries forward
 * it through `metrics[0]`. Empty string means "no client selected" — the
 * Search tab handler treats that as "show the empty-state notice" rather
 * than an error.
 */
export function clientVariable(params: { name: string; label: string }): CustomVariable {
  return new CustomVariable({
    name: params.name,
    label: params.label,
    query: '',
    value: '',
    text: '',
    includeAll: false,
    isMulti: false,
  });
}

