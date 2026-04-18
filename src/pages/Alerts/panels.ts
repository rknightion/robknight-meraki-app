import { FieldColorModeId, ThresholdsMode } from '@grafana/schema';
import {
  PanelBuilders,
  SceneDataTransformer,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';

/**
 * Severity-filter contract — read once, remember forever.
 *
 * The Go backend reuses `MerakiQuery.Metrics` (`[]string` on the wire) as
 * the severity filter for Alerts queries. The B2 agent deliberately did
 * NOT add a dedicated `severity` field to `MerakiQuery` so that the
 * Phase 6 frontend work wouldn't race the in-flight query-kind changes.
 *
 * Consequence: every Alerts / AlertsOverview query runner in this file
 * passes the severity filter as `metrics: ['$severity']`. The $severity
 * variable resolves to `''` for "All", `info`, `warning`, or `critical`.
 * The backend treats `['']` as "no filter"; see
 * `pkg/plugin/query/alerts.go` for the pass-through logic.
 *
 * This is an ugly but real contract. When the B2 landing finalises and a
 * proper `severity` field is added, update this file plus
 * `organizationAlertsScene.ts` in one commit.
 */

type AlertsQueryKind = QueryKind.Alerts | QueryKind.AlertsOverview;

interface AlertsQueryOpts {
  refId?: string;
  kind: AlertsQueryKind;
  /**
   * Hard-code an orgId for per-org-scoped panels (e.g. the Organization
   * detail Alerts tab, where the org is already resolved). When omitted,
   * the query binds to the `$org` variable from the scene.
   */
  orgId?: string;
  /**
   * Severity filter pushed through the `metrics` field. Defaults to the
   * `$severity` variable; pass an explicit value (e.g. `['']`) to bypass
   * the variable or when rendering from a scene that doesn't own one.
   */
  severity?: string[];
  maxDataPoints?: number;
}

/**
 * Build a raw SceneQueryRunner for an Alerts/AlertsOverview kind. Kept
 * local to the Alerts area so the severity-via-metrics trick doesn't leak
 * into the shared `src/scene-helpers/panels.ts` file (which uses a
 * strictly-typed `SensorMetric[]` for `metrics`).
 */
function alertsQuery(opts: AlertsQueryOpts): SceneQueryRunner {
  const {
    refId = 'A',
    kind,
    orgId,
    severity = ['$severity'],
    maxDataPoints,
  } = opts;

  const query: Record<string, unknown> & { refId: string } = {
    refId,
    kind,
    orgId: orgId ?? '$org',
    // Severity filter — see the file-level comment for why this uses
    // `metrics` instead of a dedicated `severity` field.
    metrics: severity,
  };

  return new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [query],
    ...(maxDataPoints ? { maxDataPoints } : {}),
  });
}

/**
 * Full alerts list. Columns mirror the backend frame shape from
 * `pkg/plugin/query/alerts.go`'s `handleAlerts`:
 *   occurredAt, severity, category, alertType, network_name,
 *   device_serial, device_name, title.
 *
 * Drilldown: the `device_serial` column links to the sensor detail page
 * as a fallback — per-AP / per-switch detail pages exist in C1 / C3 but
 * we don't know the product type from the alerts frame alone. Using the
 * sensor path is the least-surprising default; once a unified "device
 * resolver" URL exists (tracked in the Phase 7 plan), swap this to that.
 */
export function alertsTable(orgId?: string): VizPanel {
  const runner = alertsQuery({ kind: QueryKind.Alerts, orgId });

  // Drop verbose columns from the table but keep them available for
  // tooltips / drilldowns. Description stays hidden by default because
  // it tends to be long prose that wrecks the row height; users can see
  // it by hovering the title column.
  const organized = new SceneDataTransformer({
    $data: runner,
    transformations: [
      {
        id: 'organize',
        options: {
          excludeByName: {
            description: true,
            network_id: true,
          },
          renameByName: {},
        },
      },
    ],
  });

  return PanelBuilders.table()
    .setTitle('Alerts')
    .setDescription('Assurance alerts returned by the Meraki API for the selected organization and severity.')
    .setData(organized)
    .setNoValue('No alerts in the selected range.')
    .setOverrides((b) => {
      b.matchFieldsWithName('device_serial').overrideLinks([
        {
          // TODO: resolve the correct per-product detail URL once a
          // unified device resolver exists. Using the sensor path as a
          // fallback covers MT devices; MR/MS drilldowns land in a
          // later phase.
          title: 'Open device',
          url: `${PLUGIN_BASE_URL}/${ROUTES.Sensors}/\${__value.raw:percentencode}?var-org=\${var-org:queryparam}`,
        },
      ]);
      b.matchFieldsWithName('severity').overrideCustomFieldConfig('width', 110);
      b.matchFieldsWithName('occurredAt').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('alertType').overrideCustomFieldConfig('width', 200);
    })
    .build();
}

/**
 * Alerts timeline — bar chart of alert counts bucketed by time. Driven
 * by the same Alerts query runner as the table; a `groupingToMatrix`
 * transform pivots the (time, severity, count) shape into one field
 * per severity so the bar chart stacks naturally.
 *
 * Note: if the backend later switches to emitting pre-bucketed counts
 * from `AlertsOverview`, drop the transform and point `$data` at the
 * overview runner instead.
 */
export function alertsTimelineBarChart(orgId?: string): VizPanel {
  const runner = alertsQuery({ kind: QueryKind.Alerts, orgId });

  // Pivot into a matrix: row = occurredAt bucket, columns = severity.
  // The `groupingToMatrix` transform does this in one step. We give it
  // a placeholder value column (`severity`) and reduce by count — but
  // because `groupingToMatrix` emits NULL for empty cells, the bar
  // chart renders each severity as its own stackable series.
  const pivoted = new SceneDataTransformer({
    $data: runner,
    transformations: [
      {
        id: 'groupingToMatrix',
        options: {
          columnField: 'severity',
          rowField: 'occurredAt',
          valueField: 'severity',
          emptyValue: 'null',
        },
      },
    ],
  });

  return PanelBuilders.barchart()
    .setTitle('Alerts timeline')
    .setDescription('Alert volume over the selected time range, stacked by severity.')
    .setData(pivoted)
    .setNoValue('No alerts in the selected range.')
    .setOption('stacking', 'normal' as any)
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .setCustomFieldConfig('fillOpacity', 80)
    .setCustomFieldConfig('lineWidth', 0)
    .build();
}

/**
 * Build a single KPI stat panel from the server-side `AlertsOverview`
 * frame. Mirrors the `alertStat` helper in `src/scene-helpers/panels.ts`:
 * each panel applies an `organize` transform that keeps only the named
 * field so the stat viz picks up exactly one number.
 *
 * See `pkg/plugin/query/alerts.go`'s `handleAlertsOverview` — it emits
 * one wide frame with one field per severity. Client-side aggregation
 * (filterByValue + reduce) is deliberately avoided; see gotcha G.20 in
 * `todos.txt` for why server-side aggregation is the right shape.
 */
function alertOverviewStat(
  title: string,
  field: 'critical' | 'warning' | 'informational',
  thresholds: Array<{ value: number; color: string }>,
  orgId?: string
): VizPanel {
  const runner = alertsQuery({ kind: QueryKind.AlertsOverview, orgId });

  const excludeByName: Record<string, boolean> = {
    critical: field !== 'critical',
    warning: field !== 'warning',
    informational: field !== 'informational',
  };

  const builder = PanelBuilders.stat()
    .setTitle(title)
    .setData(
      new SceneDataTransformer({
        $data: runner,
        transformations: [{ id: 'organize', options: { excludeByName, renameByName: {} } }],
      })
    )
    .setNoValue('0')
    .setOption('reduceOptions', {
      values: false,
      calcs: ['lastNotNull'],
      fields: '',
    } as any)
    .setOption('colorMode', 'value' as any);

  if (thresholds.length > 0) {
    builder
      .setColor({ mode: FieldColorModeId.Thresholds })
      .setThresholds({
        mode: ThresholdsMode.Absolute,
        steps: thresholds.map((t, i) => ({
          value: i === 0 ? (null as unknown as number) : t.value,
          color: t.color,
        })),
      });
  }

  return builder.build();
}

/**
 * KPI row for the Alerts overview — three stat panels backed by a single
 * `AlertsOverview` query kind per panel (the overview frame is small, so
 * re-running the query per panel is cheap and keeps failures isolated
 * per-tile, matching the `orgDetailKpiRow` pattern).
 *
 * Thresholds match the Meraki UI convention: critical is red above zero,
 * warning is orange above zero, informational stays blue/neutral.
 */
export function alertsKpiRow(orgId?: string): VizPanel[] {
  return [
    alertOverviewStat(
      'Critical',
      'critical',
      [
        { value: 0, color: 'green' },
        { value: 1, color: 'red' },
      ],
      orgId
    ),
    alertOverviewStat(
      'Warning',
      'warning',
      [
        { value: 0, color: 'green' },
        { value: 1, color: 'orange' },
      ],
      orgId
    ),
    alertOverviewStat(
      'Informational',
      'informational',
      [
        { value: 0, color: 'blue' },
        { value: 1, color: 'blue' },
      ],
      orgId
    ),
  ];
}

/**
 * Small "recent alerts" tile for the Home page — a compact table of the
 * five most recent alerts. The backend returns alerts sorted
 * newest-first already (`sortOrder=descending`), so the `limit`
 * transform just trims the tail.
 *
 * Decision — how we get "top 5": the Alerts query runner does not
 * expose a per-query row limit on the wire (and pushing one down would
 * mean a new backend param just for this tile). Declarative limiting
 * via a `limit` transform is both simpler and more honest — the full
 * list is still cached server-side so drilling into the Alerts page
 * from here is fast.
 */
export function recentAlertsTile(orgId?: string): VizPanel {
  const runner = alertsQuery({
    kind: QueryKind.Alerts,
    orgId,
    // Force "All" severity for the Home tile — we want the headline
    // view, not whatever the user last filtered on another page.
    severity: [''],
  });

  const trimmed = new SceneDataTransformer({
    $data: runner,
    transformations: [
      // Drop the noisy columns first so the tile stays readable on
      // narrow viewports.
      {
        id: 'organize',
        options: {
          excludeByName: {
            description: true,
            network_id: true,
            alertType: true,
            category: true,
            device_name: true,
          },
          renameByName: {},
        },
      },
      // Then cap the row count. The backend already sorts newest-first,
      // so a straight `limit` gives us the top 5.
      {
        id: 'limit',
        options: { limitField: 5 },
      },
    ],
  });

  return PanelBuilders.table()
    .setTitle('Recent alerts')
    .setDescription('The five most recent Meraki alerts across the selected organization.')
    .setData(trimmed)
    .setNoValue('No recent alerts.')
    .setOverrides((b) => {
      b.matchFieldsWithName('device_serial').overrideLinks([
        {
          title: 'Open device',
          url: `${PLUGIN_BASE_URL}/${ROUTES.Sensors}/\${__value.raw:percentencode}?var-org=\${var-org:queryparam}`,
        },
      ]);
      b.matchFieldsWithName('severity').overrideCustomFieldConfig('width', 100);
      b.matchFieldsWithName('occurredAt').overrideCustomFieldConfig('width', 170);
    })
    .build();
}
