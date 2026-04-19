import { FieldColorModeId, ThresholdsMode } from '@grafana/schema';
import {
  PanelBuilders,
  SceneDataTransformer,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';

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

type AlertsStatus = 'active' | 'resolved' | 'dismissed' | 'all';

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
  /**
   * Lifecycle-state filter. Backend default is "active" (currently firing,
   * no time filter). Pass "all" from historical panels (timeline bar chart,
   * MTTR) so the backend applies the picker window. Maps to metrics[1].
   */
  status?: AlertsStatus;
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
    status,
    maxDataPoints,
  } = opts;

  // Severity goes through the `metrics` slot so the CSV interpolation
  // in `applyTemplateVariables` can expand `$severity` like every other
  // template var. Status uses a DEDICATED field (`alertStatus`) —
  // previously we packed it into `metrics[1]`, but `splitMulti` drops
  // empty CSV entries, so when $severity resolved to the "All" sentinel
  // (`''`) the status value shifted into metrics[0] and Meraki rejected
  // `severity=all` with HTTP 500.
  const query: Record<string, unknown> & { refId: string } = {
    refId,
    kind,
    orgId: orgId ?? '$org',
    metrics: severity,
  };
  if (status) {
    query.alertStatus = status;
  }

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
  // Show ALL alert states (active + resolved + dismissed) so operators see
  // the full picture. Resolved / dismissed alerts are colour-coded in the
  // status column (green / grey) so they're visually distinct from active
  // rows. The status: 'all' sentinel tells the backend to apply the picker
  // window via tsStart/tsEnd — users narrow to "what fired recently" by
  // shortening the time picker rather than by the status filter.
  const runner = alertsQuery({ kind: QueryKind.Alerts, orgId, status: 'all' });

  // Drop verbose columns from the table but keep them available for
  // tooltips / drilldowns. Description stays hidden because it's long
  // prose that wrecks row height; drilldownUrl / device_productType stay
  // hidden because they exist purely to power the per-row drilldown.
  const organized = new SceneDataTransformer({
    $data: runner,
    transformations: [
      {
        id: 'organize',
        options: {
          excludeByName: {
            description: true,
            network_id: true,
            device_productType: true,
            drilldownUrl: true,
          },
          renameByName: {},
        },
      },
    ],
  });

  return PanelBuilders.table()
    .setTitle('Alerts')
    .setDescription('All Meraki alerts for the selected organization and severity — active rows in red, resolved in green, dismissed in grey.')
    .setData(organized)
    // Cell-level placeholder for missing values (network-wide alerts have no
    // device_serial / device_name, and `setNoValue` in Grafana applies per
    // EMPTY CELL rather than per empty panel). A sentence-length message
    // bled into every device column — an em-dash is the least surprising
    // stand-in. The panel shows headers-without-rows when the frame itself
    // is empty, which is sufficient signal to the operator.
    .setNoValue('—')
    .setOverrides((b) => {
      // Cross-family drilldown: the backend emits one URL per row keyed on the
      // alert's device.productType, so a table spanning MR/MS/MX/MV/MG/MT
      // routes each row to the right per-family detail page (§1.12 in todos.txt).
      // When a network-wide alert has no device, drilldownUrl is empty — the viz
      // still renders the link markup but clicking it is a no-op.
      b.matchFieldsWithName('device_serial').overrideLinks([
        {
          title: 'Open device',
          url: '${__data.fields.drilldownUrl}',
        },
      ]);
      b.matchFieldsWithName('severity').overrideCustomFieldConfig('width', 110);
      b.matchFieldsWithName('occurredAt').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('alertType').overrideCustomFieldConfig('width', 200);
      // Status column colour-coded so active / resolved / dismissed are
      // visually distinct at a glance. value-to-color mapping: firing red,
      // resolved green, dismissed grey.
      b.matchFieldsWithName('status').overrideCustomFieldConfig('width', 110);
      b.matchFieldsWithName('status').overrideCustomFieldConfig(
        'cellOptions',
        { type: 'color-background', mode: 'gradient' } as any
      );
      b.matchFieldsWithName('status').overrideMappings([
        {
          type: 'value',
          options: {
            active: { text: 'Active', color: 'red', index: 0 },
            resolved: { text: 'Resolved', color: 'green', index: 1 },
            dismissed: { text: 'Dismissed', color: 'dark-blue', index: 2 },
          },
        } as any,
      ]);
    })
    .build();
}

/**
 * Alerts timeline — bar chart of alert counts bucketed by time, backed by
 * the server-side `AlertsOverviewHistorical` kind which emits one frame
 * per severity with the counts pre-bucketed by `segmentDuration`.
 *
 * Historically this used the `Alerts` list + a `groupingToMatrix` transform
 * with `valueField: 'severity'` — the transform emits string cells for a
 * string valueField, which bar/timeseries vizes render as empty. Combined
 * with Meraki's `tsStart` filter on `startedAt` (hiding active+resolved
 * alerts that started before the picker window), the panel read "No alerts
 * in the selected range" even when the table below had data. Swapping to
 * `AlertsOverviewHistorical` fixes both at once: numeric values per bucket
 * and the endpoint's own time-filter semantics, which are `segmentStart`-
 * based rather than startedAt-based.
 */
export function alertsTimelineBarChart(orgId?: string): VizPanel {
  const runner = new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [
      {
        refId: 'A',
        kind: QueryKind.AlertsOverviewHistorical,
        orgId: orgId ?? '$org',
      },
    ],
  });

  return PanelBuilders.barchart()
    .setTitle('Alerts timeline')
    .setDescription('Alert volume over the selected time range, stacked by severity. One bucket per segment returned by the `/assurance/alerts/overview/historical` endpoint.')
    .setData(runner)
    .setNoValue('No alerts in the selected range.')
    .setOption('stacking', 'normal' as any)
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .setCustomFieldConfig('fillOpacity', 80)
    .setCustomFieldConfig('lineWidth', 0)
    .setOverrides((b) => {
      b.matchFieldsWithName('critical').overrideColor({ fixedColor: 'red', mode: FieldColorModeId.Fixed });
      b.matchFieldsWithName('warning').overrideColor({ fixedColor: 'orange', mode: FieldColorModeId.Fixed });
      b.matchFieldsWithName('informational').overrideColor({ fixedColor: 'blue', mode: FieldColorModeId.Fixed });
    })
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

  // `total` is deliberately excluded alongside the other-severity fields —
  // without this the stat viz renders BOTH "critical: 0" and "total: 543"
  // inside the Critical tile, because `total` sits in the same wide frame
  // and nothing else was filtering it out.
  const excludeByName: Record<string, boolean> = {
    critical: field !== 'critical',
    warning: field !== 'warning',
    informational: field !== 'informational',
    total: true,
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
  // "Active ..." in the title makes explicit that these are a currently-
  // firing snapshot, not "alerts in the picker window" — the previous
  // wording led operators to expect the counts to change with the time
  // picker, which they intentionally don't.
  return [
    alertOverviewStat(
      'Active critical',
      'critical',
      [
        { value: 0, color: 'green' },
        { value: 1, color: 'red' },
      ],
      orgId
    ),
    alertOverviewStat(
      'Active warning',
      'warning',
      [
        { value: 0, color: 'green' },
        { value: 1, color: 'orange' },
      ],
      orgId
    ),
    alertOverviewStat(
      'Active informational',
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
    // Show all lifecycle states so freshly-resolved alerts still land
    // on the tile (users otherwise see a blank tile the moment an
    // incident clears, which is surprising).
    status: 'all',
  });

  const trimmed = new SceneDataTransformer({
    $data: runner,
    transformations: [
      // Drop the noisy columns first so the tile stays readable on
      // narrow viewports. device_productType / drilldownUrl stay hidden
      // — they back the per-row drilldown but aren't meant to be read.
      {
        id: 'organize',
        options: {
          excludeByName: {
            description: true,
            network_id: true,
            alertType: true,
            category: true,
            device_name: true,
            device_productType: true,
            drilldownUrl: true,
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
    // See alertsTable for why this is an em-dash rather than a sentence —
    // Grafana applies noValue per empty CELL, not per empty panel, so
    // network-wide alerts (no device) would otherwise bleed the message
    // into every device column.
    .setNoValue('—')
    .setOverrides((b) => {
      // Per-row cross-family drilldown (see alertsTable for why).
      b.matchFieldsWithName('device_serial').overrideLinks([
        {
          title: 'Open device',
          url: '${__data.fields.drilldownUrl}',
        },
      ]);
      b.matchFieldsWithName('severity').overrideCustomFieldConfig('width', 100);
      b.matchFieldsWithName('occurredAt').overrideCustomFieldConfig('width', 170);
    })
    .build();
}

// §4.4.3-1f — MTTR summary panels ---------------------------------------------

/**
 * Runner for the `alertsMttrSummary` wide KPI frame. One row with five fields:
 *   mttrMeanSeconds | mttrP50Seconds | mttrP95Seconds | resolvedCount | openCount
 * See `pkg/plugin/query/mttr.go::handleAlertsMttrSummary` for the emit shape.
 */
function mttrSummaryRunner(orgId?: string): SceneQueryRunner {
  return new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [
      {
        refId: 'A',
        kind: QueryKind.AlertsMttrSummary,
        orgId: orgId ?? '$org',
      },
    ],
  });
}

function mttrStat(
  title: string,
  runner: SceneQueryRunner,
  field: 'mttrMeanSeconds' | 'mttrP50Seconds' | 'mttrP95Seconds' | 'resolvedCount' | 'openCount',
  unit?: string
): VizPanel {
  const builder = PanelBuilders.stat()
    .setTitle(title)
    .setData(
      new SceneDataTransformer({
        $data: runner,
        transformations: [
          { id: 'filterFieldsByName', options: { include: { names: [field] } } },
        ],
      })
    )
    .setNoValue('0')
    .setOption('reduceOptions', {
      values: false,
      calcs: ['lastNotNull'],
      fields: '',
    } as any)
    .setOption('colorMode', 'none' as any);
  if (unit) {
    builder.setUnit(unit);
  }
  return builder.build();
}

/**
 * MTTR KPI row: mean / p50 / p95 resolution time plus resolved + open counts.
 *
 * Shared runner across all five tiles — the backend emits a single wide frame
 * with one field per KPI (todos.txt §G.20 pattern). The first three tiles use
 * Grafana's `s` (seconds) unit so the stat viz auto-scales to minutes/hours.
 */
export function alertsMttrKpiRow(orgId?: string): VizPanel[] {
  const runner = mttrSummaryRunner(orgId);
  return [
    mttrStat('MTTR mean', runner, 'mttrMeanSeconds', 's'),
    mttrStat('MTTR p50', runner, 'mttrP50Seconds', 's'),
    mttrStat('MTTR p95', runner, 'mttrP95Seconds', 's'),
    mttrStat('Resolved', runner, 'resolvedCount'),
    mttrStat('Open', runner, 'openCount'),
  ];
}

// §3.4 — Alerts overview byNetwork + historical --------------------------------

/**
 * Sortable table of alert counts per network (critical / warning / informational / total).
 * Backed by `AlertsOverviewByNetwork` which calls
 * GET /organizations/{organizationId}/assurance/alerts/overview/byNetwork.
 *
 * Color overrides: critical column is red above 0; warning is orange above 0.
 */
export function alertsByNetworkTable(orgId?: string): VizPanel {
  const runner = new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [
      {
        refId: 'A',
        kind: QueryKind.AlertsOverviewByNetwork,
        orgId: orgId ?? '$org',
      },
    ],
  });

  return PanelBuilders.table()
    .setTitle('Alerts by network')
    .setDescription('Alert severity counts per network across the selected organization.')
    .setData(runner)
    .setNoValue('—')
    .setOverrides((b) => {
      b.matchFieldsWithName('critical')
        .overrideColor({ mode: FieldColorModeId.Thresholds })
        .overrideThresholds({
          mode: ThresholdsMode.Absolute,
          steps: [
            { value: null as unknown as number, color: 'green' },
            { value: 1, color: 'red' },
          ],
        })
        .overrideCustomFieldConfig('cellOptions', { type: 'color-text' } as any);
      b.matchFieldsWithName('warning')
        .overrideColor({ mode: FieldColorModeId.Thresholds })
        .overrideThresholds({
          mode: ThresholdsMode.Absolute,
          steps: [
            { value: null as unknown as number, color: 'green' },
            { value: 1, color: 'orange' },
          ],
        })
        .overrideCustomFieldConfig('cellOptions', { type: 'color-text' } as any);
      b.matchFieldsWithName('networkId').overrideCustomFieldConfig('width', 200);
      b.matchFieldsWithName('networkName').overrideCustomFieldConfig('width', 220);
      b.matchFieldsWithName('critical').overrideCustomFieldConfig('width', 90);
      b.matchFieldsWithName('warning').overrideCustomFieldConfig('width', 90);
      b.matchFieldsWithName('informational').overrideCustomFieldConfig('width', 120);
      b.matchFieldsWithName('total').overrideCustomFieldConfig('width', 80);
    })
    .build();
}

/**
 * Stacked area timeseries of alert severity counts over time.
 * One frame per severity (critical / warning / informational) with labels;
 * Grafana stacks them because each series is a separate frame with labels on
 * the value field (native timeseries shape). Backed by `AlertsOverviewHistorical`.
 */
export function alertsHistoricalTimeseries(orgId?: string): VizPanel {
  const runner = new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [
      {
        refId: 'A',
        kind: QueryKind.AlertsOverviewHistorical,
        orgId: orgId ?? '$org',
      },
    ],
  });

  return PanelBuilders.timeseries()
    .setTitle('Alert history by severity')
    .setDescription(
      'Historical alert counts bucketed by severity over the selected time range.'
    )
    .setData(runner)
    .setNoValue('No historical alert data available.')
    .setCustomFieldConfig('stacking', { mode: 'normal' } as any)
    .setCustomFieldConfig('fillOpacity', 40)
    .setCustomFieldConfig('lineWidth', 1)
    .setOption('legend', {
      showLegend: true,
      displayMode: 'list',
      placement: 'bottom',
    } as any)
    .setOverrides((b) => {
      b.matchFieldsWithName('critical').overrideColor({ fixedColor: 'red', mode: FieldColorModeId.Fixed });
      b.matchFieldsWithName('warning').overrideColor({ fixedColor: 'orange', mode: FieldColorModeId.Fixed });
      b.matchFieldsWithName('informational').overrideColor({ fixedColor: 'blue', mode: FieldColorModeId.Fixed });
    })
    .build();
}
