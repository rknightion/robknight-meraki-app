import { QueryVariable } from '@grafana/scenes';
import { VariableRefresh } from '@grafana/schema';
import { MERAKI_DS_REF } from './datasource';
import { QueryKind } from '../datasource/types';

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

