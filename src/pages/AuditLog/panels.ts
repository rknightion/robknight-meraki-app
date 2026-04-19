import {
  PanelBuilders,
  SceneDataTransformer,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { FieldColorModeId } from '@grafana/schema';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';

// Shared query-runner factory -------------------------------------------------

interface AuditLogQueryOpts {
  refId?: string;
  kind: QueryKind.ConfigurationChanges | QueryKind.ConfigurationChangesTimeline;
  maxDataPoints?: number;
}

/**
 * Build a `SceneQueryRunner` for a ConfigurationChanges-family kind. Variables
 * are baked in so both panels on the audit-log page share exactly the same
 * shape:
 *   - orgId: `$org`
 *   - metrics: `['$admin']` — admin-id filter (empty string → no filter)
 *
 * Deliberately NO network filter: Meraki's /configurationChanges endpoint
 * treats `networkId=…` as "show only network-scoped changes for this
 * network", which excludes org-level changes entirely. Most audit entries
 * in a typical org are org-level (`networkId: null`), so filtering by any
 * specific network on a single-network org returns zero rows. Keeping the
 * page org-scoped matches Meraki Dashboard's own audit view.
 */
function configurationChangesQuery(opts: AuditLogQueryOpts): SceneQueryRunner {
  const { refId = 'A', kind, maxDataPoints } = opts;
  return new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [
      {
        refId,
        kind,
        orgId: '$org',
        metrics: ['$admin'],
      },
    ],
    ...(maxDataPoints ? { maxDataPoints } : {}),
  });
}

/**
 * Change-volume timeline — one stacked bar per time bucket, one series per
 * `page` value. Backed by the server-side `ConfigurationChangesTimeline`
 * aggregator which emits a wide numeric frame `{ts, <page1>, <page2>, ...}`
 * with zero-filled buckets; the viz gets real numeric fields so it no
 * longer reports "missing a number field" (the previous client-side
 * `groupingToMatrix` pivot emitted string cells). Mirrors the
 * events-timeline pattern — see `src/pages/Events/panels.ts`.
 */
export function auditLogTimelineBarChart(): VizPanel {
  const runner = configurationChangesQuery({ kind: QueryKind.ConfigurationChangesTimeline });

  return PanelBuilders.timeseries()
    .setTitle('Change volume')
    .setDescription('Configuration-change events over the selected window, stacked by page.')
    .setData(runner)
    .setNoValue('No configuration changes in the selected range.')
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .setColor({ mode: FieldColorModeId.PaletteClassic })
    .setCustomFieldConfig('drawStyle', 'bars' as any)
    .setCustomFieldConfig('fillOpacity', 80)
    .setCustomFieldConfig('lineWidth', 0)
    .setCustomFieldConfig('stacking', { mode: 'normal', group: 'A' } as any)
    .build();
}

/**
 * Full change-log table — one row per configuration change in the selected
 * window. The backend emits:
 *
 *   ts, adminName, adminEmail, adminId, networkName, networkId, ssidName,
 *   ssidNumber, page, label, oldValue, newValue, clientType, networkUrl
 *
 * Visibility + ordering:
 *  - `adminId`, `networkId`, `networkUrl` are hidden by default (available
 *    via the field inspector) — operators read the friendly names.
 *  - `oldValue` / `newValue` are shown so readers see *what* changed without
 *    opening the field inspector. They hold raw JSON strings for API edits
 *    and quoted scalars for dashboard edits; Grafana's auto cell renderer
 *    truncates with a hover tooltip, and the inspect icon opens the full
 *    payload for big diffs.
 *  - Per-field `noValue: '—'` overrides keep null cells (e.g. org-level
 *    changes have no networkName/ssidName/clientType) rendering as an em-
 *    dash instead of bleeding into the panel-level "no data" message.
 *    That's why we do NOT call `setNoValue()` at the panel level — on the
 *    table viz, Grafana applies panel-level noValue per cell, so setting
 *    it there made every null cell read "No configuration changes in the
 *    selected range."
 */
export function auditLogTable(): VizPanel {
  const runner = configurationChangesQuery({ kind: QueryKind.ConfigurationChanges });

  const organized = new SceneDataTransformer({
    $data: runner,
    transformations: [
      {
        id: 'organize',
        options: {
          excludeByName: {
            adminId: true,
            networkId: true,
            networkUrl: true,
          },
          indexByName: {
            ts: 0,
            adminName: 1,
            adminEmail: 2,
            networkName: 3,
            ssidName: 4,
            ssidNumber: 5,
            clientType: 6,
            page: 7,
            label: 8,
            oldValue: 9,
            newValue: 10,
          },
          renameByName: {
            ts: 'Time',
            adminName: 'Admin',
            adminEmail: 'Email',
            networkName: 'Network',
            ssidName: 'SSID',
            ssidNumber: 'SSID #',
            clientType: 'Client',
            page: 'Source',
            label: 'Change',
            oldValue: 'Old value',
            newValue: 'New value',
          },
        },
      },
    ],
  });

  return PanelBuilders.table()
    .setTitle('Configuration changes')
    .setDescription('Audit log of Meraki Dashboard configuration changes for the selected organization.')
    .setData(organized)
    .setOverrides((b) => {
      b.matchFieldsWithName('Time').overrideCustomFieldConfig('width', 190);
      b.matchFieldsWithName('Admin').overrideCustomFieldConfig('width', 160);
      b.matchFieldsWithName('Email').overrideCustomFieldConfig('width', 220);
      b.matchFieldsWithName('Network').overrideCustomFieldConfig('width', 180).overrideNoValue('— org-wide');
      b.matchFieldsWithName('SSID').overrideCustomFieldConfig('width', 140).overrideNoValue('—');
      b.matchFieldsWithName('SSID #').overrideCustomFieldConfig('width', 80).overrideNoValue('—');
      b.matchFieldsWithName('Client').overrideCustomFieldConfig('width', 110).overrideNoValue('dashboard');
      b.matchFieldsWithName('Source').overrideCustomFieldConfig('width', 160);
      b.matchFieldsWithName('Change').overrideCustomFieldConfig('width', 260);
      // oldValue / newValue hold JSON blobs — give them room and enable cell
      // inspect so the operator can pop the full payload.
      b.matchFieldsWithName('Old value').overrideCustomFieldConfig('inspect', true as any).overrideNoValue('—');
      b.matchFieldsWithName('New value').overrideCustomFieldConfig('inspect', true as any).overrideNoValue('—');
    })
    .setOption('sortBy', [{ displayName: 'Time', desc: true }] as any)
    .build();
}
