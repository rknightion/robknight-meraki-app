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
  maxDataPoints?: number;
}

/**
 * Build a `SceneQueryRunner` for the ConfigurationChanges kind. Variables
 * are baked in so both panels in this file share exactly the same shape:
 *   - orgId: `$org`
 *   - networkIds: `['$network']`
 *   - metrics: `['$admin']` — admin-id filter (empty string → no filter)
 *
 * Backend frame shape (documented):
 *   (ts, adminName, adminEmail, adminId, page, label, networkId,
 *    oldValue, newValue)
 */
function configurationChangesQuery(opts: AuditLogQueryOpts = {}): SceneQueryRunner {
  const { refId = 'A', maxDataPoints } = opts;
  return new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [
      {
        refId,
        kind: QueryKind.ConfigurationChanges,
        orgId: '$org',
        networkIds: ['$network'],
        metrics: ['$admin'],
      },
    ],
    ...(maxDataPoints ? { maxDataPoints } : {}),
  });
}

/**
 * Change-volume timeline — one bar per time bucket, counting the rows
 * emitted by the backend frame. We pivot through `groupingToMatrix` with
 * a placeholder column value so the bar chart counts occurrences per
 * admin bucket naturally; empty frames simply render the "No changes"
 * noValue placeholder without exploding the transform pipeline.
 *
 * Mirrors `alertsTimelineBarChart` / `eventsTimelineBarChart` so the
 * visual density matches the rest of the app's "timeline" surfaces.
 */
export function auditLogTimelineBarChart(): VizPanel {
  const runner = configurationChangesQuery();

  const pivoted = new SceneDataTransformer({
    $data: runner,
    transformations: [
      {
        id: 'groupingToMatrix',
        options: {
          columnField: 'page',
          rowField: 'ts',
          valueField: 'page',
          emptyValue: 'null',
        },
      },
    ],
  });

  return PanelBuilders.timeseries()
    .setTitle('Change volume')
    .setDescription('Configuration-change events over the selected window, stacked by page.')
    .setData(pivoted)
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
 * Full change-log table — one row per configuration change in the
 * selected window. Columns follow the backend frame shape:
 *   ts, adminName, adminEmail, adminId, page, label, networkId,
 *   oldValue, newValue
 *
 * `adminId`, `oldValue`, and `newValue` are hidden by default so the
 * main view stays compact; they remain available in the field inspector
 * for drilldown or copy-paste. The `ts` column is the default sort —
 * Grafana's table viz sorts descending naturally when the frame arrives
 * sorted newest-first from the backend, but we also pin the override so
 * client-side re-sorting preserves newest-first as the resting state.
 */
export function auditLogTable(): VizPanel {
  const runner = configurationChangesQuery();

  const organized = new SceneDataTransformer({
    $data: runner,
    transformations: [
      {
        id: 'organize',
        options: {
          excludeByName: {
            adminId: true,
            oldValue: true,
            newValue: true,
          },
          renameByName: {},
        },
      },
    ],
  });

  return PanelBuilders.table()
    .setTitle('Configuration changes')
    .setDescription('Audit log of Meraki Dashboard configuration changes for the selected organization.')
    .setData(organized)
    .setNoValue('No configuration changes in the selected range.')
    .setOverrides((b) => {
      b.matchFieldsWithName('ts').overrideCustomFieldConfig('width', 190);
      b.matchFieldsWithName('adminName').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('adminEmail').overrideCustomFieldConfig('width', 220);
      b.matchFieldsWithName('page').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('label').overrideCustomFieldConfig('width', 220);
      b.matchFieldsWithName('networkId').overrideCustomFieldConfig('width', 180);
    })
    .setOption('sortBy', [{ displayName: 'ts', desc: true }] as any)
    .build();
}
