import { FieldColorModeId, ThresholdsMode } from '@grafana/schema';
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

// §4.4.3-1f — "What changed in 24 hours" Home tile -----------------------------

/**
 * STUB — the polished Home panel lands in §4.4.5 (Home merge). For now this
 * factory just wires the new `orgChangeFeed` query kind behind a small table
 * so the Go handler has a visible integration point. §4.4.5 will style the
 * cells (icon column for audit-vs-event, severity-coloured text, drilldown
 * links back to the audit / events pages).
 *
 * The backend handler always looks back 24 hours regardless of dashboard
 * time range, and caps the frame at 10 rows sorted newest-first — the panel
 * is a "what just changed" glance, not a navigator.
 */
export function orgChangeFeedTile(): VizPanel {
  const runner = new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [
      {
        refId: 'A',
        kind: QueryKind.OrgChangeFeed,
        orgId: '$org',
      },
    ],
  });

  return PanelBuilders.table()
    .setTitle('What changed in the last 24 hours')
    .setDescription(
      'Union of configuration changes (admin-initiated) and network events ' +
        '(warning+ severity) across the selected organization. Fixed 24-hour ' +
        'lookback; ignores the dashboard time picker. §4.4.5 will polish this tile.'
    )
    .setData(runner)
    .setNoValue('Nothing changed in the last 24 hours.')
    .setOverrides((b) => {
      // Minimal cosmetic override: colour the severity column so the warning /
      // critical rows stand out in the stub. §4.4.5 will replace this with the
      // full icon/link treatment.
      b.matchFieldsWithName('severity')
        .overrideColor({ mode: FieldColorModeId.Thresholds })
        .overrideThresholds({
          mode: ThresholdsMode.Absolute,
          steps: [
            { value: null as unknown as number, color: 'blue' },
            { value: 1, color: 'orange' },
          ],
        })
        .overrideCustomFieldConfig('cellOptions', { type: 'color-text' } as any);
      b.matchFieldsWithName('time').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('source').overrideCustomFieldConfig('width', 80);
      b.matchFieldsWithName('severity').overrideCustomFieldConfig('width', 100);
    })
    .build();
}
