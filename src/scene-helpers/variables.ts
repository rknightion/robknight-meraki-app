import { QueryVariable, SceneVariableSet, TextBoxVariable } from '@grafana/scenes';
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
 * Sensor inventory filter bar. Three free-text variables backing the
 * name/serial/tag search on the Sensors overview page — bound to the
 * `sensorInventoryTable` via a `filterByValue` regex transform that ANDs
 * them. Empty values interpolate to an empty regex, which JS's `RegExp`
 * treats as "matches everything" — so a blank box is a no-op rather than
 * a zero-result filter. Keeping these three as siblings (rather than a
 * single compound box) matches the user expectation of filtering on
 * distinct fields and lets operators clear one input without losing the
 * others. The picker labels use sentence-case to match the scene's
 * existing `Organization` / `Network` variables.
 */
export function sensorNameFilterVariable(): TextBoxVariable {
  return new TextBoxVariable({
    name: 'sensorName',
    label: 'Name contains',
    value: '',
  });
}

export function sensorSerialFilterVariable(): TextBoxVariable {
  return new TextBoxVariable({
    name: 'sensorSerial',
    label: 'Serial contains',
    value: '',
  });
}

export function sensorTagFilterVariable(): TextBoxVariable {
  return new TextBoxVariable({
    name: 'sensorTag',
    label: 'Tag contains',
    value: '',
  });
}

/**
 * `clientVariable` — free-form text variable used by the Clients page for
 * MAC search and per-client drilldown.
 *
 * TextBoxVariable (not CustomVariable): there is no Meraki API that
 * enumerates every client across an org without already knowing one (the
 * org-wide /clients/search call requires a `mac` parameter), so we need a
 * text input, not a dropdown of fixed options. Operators paste the MAC (or
 * partial MAC) and the panel queries forward it through `metrics[0]`. Empty
 * string means "no client selected" — the Search tab handler treats that as
 * "show the empty-state notice" rather than an error.
 */
export function clientVariable(params: {
  name: string;
  label: string;
  value?: string;
}): TextBoxVariable {
  return new TextBoxVariable({
    name: params.name,
    label: params.label,
    value: params.value ?? '',
  });
}

