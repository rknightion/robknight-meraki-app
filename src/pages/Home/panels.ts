import {
  PanelBuilders,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';

// §3.3 — Device memory pressure timeseries ------------------------------------

/**
 * Timeseries panel showing memory usage % per device across the selected org.
 * All devices are rendered as individual series (one per serial); users can
 * hover to identify high-memory devices. Backed by `DeviceMemoryHistory`
 * which calls GET /organizations/{organizationId}/devices/system/memory/usage/history/byInterval.
 *
 * Scope: $org + optional $network filter.
 */
export function deviceMemoryPressureTimeseries(networkId?: string): VizPanel {
  const query: Record<string, unknown> & { refId: string } = {
    refId: 'A',
    kind: QueryKind.DeviceMemoryHistory,
    orgId: '$org',
  };
  if (networkId) {
    query.networkIds = [networkId];
  }

  const runner = new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [query],
  });

  return PanelBuilders.timeseries()
    .setTitle('Device memory usage')
    .setDescription(
      'Maximum memory usage % per device over the selected time range. ' +
        'Each line is one device serial. Hover to identify high-memory devices.'
    )
    .setData(runner)
    .setNoValue('No memory usage data available for the selected range.')
    .setCustomFieldConfig('fillOpacity', 10)
    .setCustomFieldConfig('lineWidth', 1)
    .setOption('legend', {
      showLegend: true,
      displayMode: 'list',
      placement: 'bottom',
    } as any)
    .setOverrides((b) => {
      b.matchFieldsByQuery('A').overrideUnit('percent').overrideMin(0).overrideMax(100);
    })
    .build();
}
