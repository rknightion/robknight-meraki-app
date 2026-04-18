import { QueryVariable } from '@grafana/scenes';
import { VariableRefresh } from '@grafana/schema';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';

/**
 * $switch — single-select MS picker hydrated from the Meraki Devices
 * metricFind handler with `productTypes=['switch']`. Used on overview scenes
 * so users can pin a panel to one switch without cascading through the
 * drilldown.
 *
 * Kept single-select because the per-port endpoints (port config, packet
 * counters) only accept one serial at a time; multi-select would force
 * panels to fan out into N frames per series and break the legend contract.
 */
export function switchVariable(): QueryVariable {
  return new QueryVariable({
    name: 'switch',
    label: 'Switch',
    datasource: MERAKI_DS_REF,
    query: {
      kind: QueryKind.Devices,
      refId: 'switches',
      orgId: '$org',
      productTypes: ['switch'],
    },
    includeAll: true,
    defaultToAll: true,
    allValue: '',
    isMulti: false,
    refresh: VariableRefresh.onDashboardLoad,
    sort: 1,
  });
}
