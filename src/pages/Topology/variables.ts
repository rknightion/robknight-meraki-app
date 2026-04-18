import { QueryVariable } from '@grafana/scenes';
import { VariableRefresh } from '@grafana/schema';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';

/**
 * $network — single-select for the Topology page's link-graph row.
 *
 * §4.4.4-D explicitly gates the LLDP/CDP fan-out to per-network scope
 * (DO NOT attempt org-wide fan-out by default). Single-select enforces
 * that contract at the UI layer: the panel binds to one network at a
 * time so the per-device fan-out budget stays bounded.
 *
 * The Geomap row (Row 1) is org-scoped via `$org` and ignores this
 * variable.
 */
export function topologyNetworkVariable(): QueryVariable {
  return new QueryVariable({
    name: 'network',
    label: 'Network',
    datasource: MERAKI_DS_REF,
    query: { kind: QueryKind.Networks, refId: 'networks', orgId: '$org' },
    includeAll: false,
    isMulti: false,
    refresh: VariableRefresh.onDashboardLoad,
    sort: 1,
  });
}
