import { FieldColorModeId, ThresholdsMode } from '@grafana/schema';
import {
  PanelBuilders,
  SceneDataTransformer,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';
import type { MerakiProductType, SensorMetric } from '../../types';

// Shared query-runner factory -------------------------------------------------
//
// Mirrors `oneQuery(...)` in `src/scene-helpers/panels.ts`. We keep a local
// copy so the Insights area doesn't depend on an internal helper in the
// shared panels module — the same convention as AccessPoints and Switches.

interface QueryParams {
  refId?: string;
  kind: QueryKind;
  orgId?: string;
  networkIds?: string[];
  serials?: string[];
  productTypes?: MerakiProductType[];
  metrics?: SensorMetric[] | string[];
  timespanSeconds?: number;
  maxDataPoints?: number;
}

function oneQuery(params: QueryParams): SceneQueryRunner {
  const {
    refId = 'A',
    kind,
    orgId,
    networkIds,
    serials,
    productTypes,
    metrics,
    timespanSeconds,
    maxDataPoints,
  } = params;

  const query: Record<string, unknown> & { refId: string } = { refId, kind };
  if (kind !== QueryKind.Organizations) {
    query.orgId = orgId ?? '$org';
  }
  if (networkIds && networkIds.length > 0) {
    query.networkIds = networkIds;
  }
  if (serials && serials.length > 0) {
    query.serials = serials;
  }
  if (productTypes && productTypes.length > 0) {
    query.productTypes = productTypes;
  }
  if (metrics && metrics.length > 0) {
    query.metrics = metrics;
  }
  if (typeof timespanSeconds === 'number') {
    query.timespanSeconds = timespanSeconds;
  }

  return new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [query],
    ...(maxDataPoints ? { maxDataPoints } : {}),
  });
}

function hideColumns(runner: SceneQueryRunner, columns: string[]): SceneDataTransformer {
  const excludeByName: Record<string, boolean> = {};
  for (const c of columns) {
    excludeByName[c] = true;
  }
  return new SceneDataTransformer({
    $data: runner,
    transformations: [{ id: 'organize', options: { excludeByName, renameByName: {} } }],
  });
}

// Shared constants -----------------------------------------------------------

/**
 * Threshold steps for the `daysUntilExpiry` column on the licenses table.
 *
 * The steps are ordered "healthiest first": null-valued first step means
 * "green baseline"; 30d transitions to orange; 7d transitions to red. Set as
 * a readonly tuple so it can be shared with other callers (e.g. future
 * heat-map panels) without drift.
 */
export const LICENSE_EXPIRY_THRESHOLDS = [
  { value: null, color: 'green' },
  { value: 30, color: 'orange' },
  { value: 7, color: 'red' },
] as const;

// Shared helpers -------------------------------------------------------------

/**
 * `topTable` consolidates the five nearly-identical top-N table panels on the
 * Clients tab (top clients / top devices / top SSIDs / top device models / top
 * switches by energy). Each of those only differs by the backing query kind,
 * which columns to hide, the column widths, and whether the serial column
 * should carry a drilldown link — all pushed through this one helper so the
 * scene file stays declarative.
 *
 * `serialColumn` names the field that should receive the drilldownUrl-based
 * "Open device" link. Only the top_devices / top_switches_by_energy frames
 * carry a `drilldownUrl` column; for the other frames we leave the param
 * undefined and no link is applied.
 */
export function topTable(params: {
  title: string;
  description?: string;
  kind: QueryKind;
  /** Columns to exclude via an `organize` transform. Defaults to none. */
  hideColumns?: string[];
  /** Optional per-column width overrides (field name → pixels). */
  widths?: Record<string, number>;
  /**
   * When set, adds an "Open device" override link on the named column using
   * `${__data.fields.drilldownUrl}`. Only meaningful for frames that carry a
   * `drilldownUrl` field (top_devices, top_switches_by_energy).
   */
  serialColumn?: string;
}): VizPanel {
  const runner = oneQuery({ kind: params.kind });
  const data = params.hideColumns && params.hideColumns.length > 0
    ? hideColumns(runner, params.hideColumns)
    : runner;

  const builder = PanelBuilders.table()
    .setTitle(params.title)
    .setData(data)
    .setNoValue('No data in the selected window.');

  if (params.description) {
    builder.setDescription(params.description);
  }

  builder.setOverrides((b) => {
    if (params.serialColumn) {
      b.matchFieldsWithName(params.serialColumn).overrideLinks([
        {
          title: 'Open device',
          url: '${__data.fields.drilldownUrl}',
        },
      ]);
    }
    if (params.widths) {
      for (const [name, width] of Object.entries(params.widths)) {
        b.matchFieldsWithName(name).overrideCustomFieldConfig('width', width);
      }
    }
  });

  return builder.build();
}

// Licensing panels -----------------------------------------------------------

/**
 * Build one stat on the licensing KPI row. All four tiles share a single
 * `LicensesOverview` runner — a `filterFieldsByName` transform isolates the
 * column each tile should display. Re-using one runner keeps the server load
 * down and guarantees the four tiles report a consistent snapshot.
 */
function licensingStat(
  title: string,
  runner: SceneQueryRunner,
  field: 'active' | 'expiring30' | 'expired' | 'total',
  thresholds: Array<{ value: number | null; color: string }>
): VizPanel {
  const builder = PanelBuilders.stat()
    .setTitle(title)
    .setData(
      new SceneDataTransformer({
        $data: runner,
        transformations: [
          {
            id: 'filterFieldsByName',
            options: {
              include: { names: [field] },
            },
          },
        ],
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
          value: i === 0 ? (null as unknown as number) : (t.value as number),
          color: t.color,
        })),
      });
  }

  return builder.build();
}

/**
 * KPI row for the Licensing tab: active, expiring (≤30d), expired, total.
 *
 * A single `LicensesOverview` runner is shared across the four tiles (the
 * overview frame is a wide one-row shape with all columns present). Each tile
 * filters to its column via `filterFieldsByName` — that keeps the backend
 * call count at one per refresh regardless of how many KPI cards we render.
 */
export function licensingKpiRow(): VizPanel[] {
  const runner = oneQuery({ kind: QueryKind.LicensesOverview });

  return [
    licensingStat('Active licenses', runner, 'active', [
      { value: 0, color: 'green' },
    ]),
    licensingStat('Expiring (≤30d)', runner, 'expiring30', [
      { value: null, color: 'green' },
      { value: 1, color: 'orange' },
      { value: 30, color: 'red' },
    ]),
    licensingStat('Expired', runner, 'expired', [
      { value: null, color: 'green' },
      { value: 1, color: 'red' },
    ]),
    licensingStat('Total licenses', runner, 'total', []),
  ];
}

/**
 * Stat card showing the co-termination expiration date. For per-device orgs
 * the backend emits a zero `cotermExpiration`, which Grafana renders as the
 * "no value" placeholder set via `.setNoValue('—')`. We don't conditionally
 * hide this panel based on `coterm=true` — a single-tile "—" for per-device
 * orgs is less surprising than an empty gap appearing on co-term orgs.
 */
export function cotermExpirationStat(): VizPanel {
  const runner = oneQuery({ kind: QueryKind.LicensesOverview });

  // Keep only the cotermExpiration column so the stat viz picks up exactly
  // one time value.
  const data = new SceneDataTransformer({
    $data: runner,
    transformations: [
      {
        id: 'organize',
        options: {
          excludeByName: {
            active: true,
            expiring30: true,
            expired: true,
            recentlyQueued: true,
            unusedActive: true,
            total: true,
            coterm: true,
            cotermStatus: true,
          },
          renameByName: {},
        },
      },
    ],
  });

  return PanelBuilders.stat()
    .setTitle('Co-term expiration')
    .setDescription('For co-terminated organizations, the date all licenses expire. Shows "—" for per-device orgs.')
    .setData(data)
    .setUnit('dateTimeFromNow')
    .setNoValue('—')
    .setOption('reduceOptions', {
      values: false,
      calcs: ['lastNotNull'],
      fields: '',
    } as any)
    .setOption('colorMode', 'none' as any)
    .build();
}

/**
 * Per-license table. Filters by `$licenseState` (defaulting to "All", which
 * the backend treats as unset). `daysUntilExpiry` gets the shared
 * `LICENSE_EXPIRY_THRESHOLDS` ramp with a color-background cell so expiring
 * rows jump out at a glance.
 *
 * `headLicenseId` is hidden by default — it's only useful when a license has
 * been renewed and linked to a predecessor, and mostly clutters the standard
 * inventory view. Users who need it can un-hide via the field config.
 *
 * No drilldown on `deviceSerial`: the licenses frame doesn't carry a
 * `drilldownUrl` column, and hard-coding a cross-product path would guess
 * wrong more often than not. Kept unlinked for v0.3.0; a future iteration
 * can resolve the correct per-product detail URL.
 */
export function licensesTable(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.LicensesList,
    metrics: ['$licenseState'],
  });

  const data = hideColumns(runner, ['headLicenseId']);

  return PanelBuilders.table()
    .setTitle('Licenses')
    .setDescription('Per-device license inventory. Co-term organizations emit an empty frame with an info notice.')
    .setData(data)
    .setNoValue('No licenses visible for the selected state.')
    .setOverrides((b) => {
      b.matchFieldsWithName('daysUntilExpiry')
        .overrideThresholds({
          mode: ThresholdsMode.Absolute,
          steps: LICENSE_EXPIRY_THRESHOLDS.map((t) => ({
            value: t.value as number,
            color: t.color,
          })),
        })
        .overrideCustomFieldConfig('cellOptions', { type: 'color-background' } as any);
      b.matchFieldsWithName('expirationDate').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('activationDate').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('state').overrideCustomFieldConfig('width', 120);
      b.matchFieldsWithName('licenseType').overrideCustomFieldConfig('width', 160);
    })
    .build();
}

// §4.4.3-1f — license renewal calendar ----------------------------------------

/**
 * Status-list-ish panel over the existing `licensesList` frame, focused on the
 * days-to-expiry column. Uses the shared `LICENSE_EXPIRY_THRESHOLDS` ramp so
 * licenses close to renewal render red, farther-off licenses render green.
 *
 * No new query kind — this is a different visualisation of the same feed that
 * backs `licensesTable`. It sits above the full inventory table on the
 * licensing tab so operators see the "who needs attention" summary first.
 *
 * Scope: filtered to the `$licenseState` variable (matching `licensesTable`),
 * hides ids / pagination columns, and adds a colour-background cell on
 * `daysUntilExpiry` for a visual calendar feel.
 */
export function licenseRenewalCalendar(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.LicensesList,
    metrics: ['$licenseState'],
  });

  const data = hideColumns(runner, [
    'headLicenseId',
    'id',
    'activationDate',
    'seatCount',
    'networkId',
  ]);

  return PanelBuilders.table()
    .setTitle('License renewal calendar')
    .setDescription(
      'License inventory sorted by days-to-expiry. Red cells indicate licenses ' +
        'expiring within 7 days; orange within 30; green otherwise.'
    )
    .setData(data)
    .setNoValue('No licenses visible for the selected state.')
    .setOverrides((b) => {
      b.matchFieldsWithName('daysUntilExpiry')
        .overrideThresholds({
          mode: ThresholdsMode.Absolute,
          steps: LICENSE_EXPIRY_THRESHOLDS.map((t) => ({
            value: t.value as number,
            color: t.color,
          })),
        })
        .overrideCustomFieldConfig('cellOptions', { type: 'color-background' } as any);
      b.matchFieldsWithName('expirationDate').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('licenseType').overrideCustomFieldConfig('width', 160);
      b.matchFieldsWithName('state').overrideCustomFieldConfig('width', 120);
      b.matchFieldsWithName('daysUntilExpiry').overrideCustomFieldConfig('width', 150);
    })
    .build();
}

// API Usage panels -----------------------------------------------------------

/**
 * One stat on the API requests KPI row. All five tiles share a single
 * `ApiRequestsOverview` runner; `filterFieldsByName` picks the column each
 * tile should display. Same runner-sharing pattern as `licensingKpiRow` —
 * one server call regardless of tile count.
 */
function apiRequestStat(
  title: string,
  runner: SceneQueryRunner,
  field: 'total' | 'success2xx' | 'clientError4xx' | 'tooMany429' | 'serverError5xx',
  thresholds: Array<{ value: number | null; color: string }>
): VizPanel {
  const builder = PanelBuilders.stat()
    .setTitle(title)
    .setData(
      new SceneDataTransformer({
        $data: runner,
        transformations: [
          {
            id: 'filterFieldsByName',
            options: { include: { names: [field] } },
          },
        ],
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
          value: i === 0 ? (null as unknown as number) : (t.value as number),
          color: t.color,
        })),
      });
  }

  return builder.build();
}

/**
 * KPI row for the API Usage tab: total / 2xx / 4xx / 429 / 5xx counters.
 *
 * Colour convention matches the Meraki dashboard UI: 2xx is green, 4xx is
 * orange, 429 and 5xx are red. Zero values render with the first-step color
 * so an all-clear estate shows four green tiles.
 */
export function apiRequestsKpiRow(): VizPanel[] {
  const runner = oneQuery({ kind: QueryKind.ApiRequestsOverview });

  return [
    apiRequestStat('Total requests', runner, 'total', []),
    apiRequestStat('2xx success', runner, 'success2xx', [
      { value: 0, color: 'green' },
    ]),
    apiRequestStat('4xx errors', runner, 'clientError4xx', [
      { value: null, color: 'green' },
      { value: 1, color: 'orange' },
    ]),
    apiRequestStat('429 rate limited', runner, 'tooMany429', [
      { value: null, color: 'green' },
      { value: 1, color: 'red' },
    ]),
    apiRequestStat('5xx server errors', runner, 'serverError5xx', [
      { value: null, color: 'green' },
      { value: 1, color: 'red' },
    ]),
  ];
}

/**
 * Stacked bar chart of API request counts bucketed by interval, one bar
 * segment per HTTP class (2xx / 4xx / 429 / 5xx). The backend emits one frame
 * per class with a labelled value field and a baked `DisplayNameFromDS`
 * (see `handleApiRequestsByInterval` in `pkg/plugin/query/insights.go`), so
 * we don't need a client-side pivot transform.
 *
 * Per-class colour overrides use `matchFieldsWithNameByRegex` on the baked
 * display name so each series renders in its semantic colour regardless of
 * the order the backend returned the frames.
 */
export function apiRequestsByIntervalChart(): VizPanel {
  return PanelBuilders.barchart()
    .setTitle('API requests by interval')
    .setDescription('Stacked bar chart of API requests, bucketed by interval and coloured by HTTP status class.')
    .setData(
      oneQuery({
        kind: QueryKind.ApiRequestsByInterval,
        maxDataPoints: 300,
      })
    )
    .setNoValue('No API activity in the selected range.')
    .setOption('stacking', 'normal' as any)
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .setCustomFieldConfig('fillOpacity', 80)
    .setCustomFieldConfig('lineWidth', 0)
    .setOverrides((b) => {
      b.matchFieldsWithNameByRegex('.*2xx.*').overrideColor({
        mode: 'fixed',
        fixedColor: 'green',
      } as any);
      b.matchFieldsWithNameByRegex('.*4xx.*').overrideColor({
        mode: 'fixed',
        fixedColor: 'orange',
      } as any);
      b.matchFieldsWithNameByRegex('.*429.*').overrideColor({
        mode: 'fixed',
        fixedColor: 'red',
      } as any);
      b.matchFieldsWithNameByRegex('.*5xx.*').overrideColor({
        mode: 'fixed',
        fixedColor: 'purple',
      } as any);
    })
    .build();
}

// §4.4.3-1f — API usage estimation (request-rate + 429 overlay) ---------------

/**
 * Timeseries panel that reshapes the existing `apiRequestsByInterval` frames
 * into a request-rate view with the 429 class as a highlighted overlay.
 *
 * The handler already emits one frame per HTTP class (`2xx`, `4xx`, `429`,
 * `5xx`) with a baked `DisplayNameFromDS`. This panel stacks the non-429
 * classes as a cumulative request-rate area and renders `429` as a red
 * overlay line — operators spot the rate-limit bumps against the background
 * traffic curve. NO new backend kind: pure visualisation override.
 *
 * Sits alongside the existing stacked bar chart on the API Usage tab — the
 * two views answer different questions (volume distribution vs rate + 429
 * pressure).
 */
export function apiRequestRateWith429Overlay(): VizPanel {
  return PanelBuilders.timeseries()
    .setTitle('API request rate + 429 overlay')
    .setDescription(
      'Request rate by HTTP class with 429 rate-limited responses overlaid ' +
        'as a red line. Spikes in the 429 overlay indicate the org is ' +
        'approaching (or hitting) its Meraki API quota.'
    )
    .setData(
      oneQuery({
        kind: QueryKind.ApiRequestsByInterval,
        maxDataPoints: 300,
      })
    )
    .setNoValue('No API activity in the selected range.')
    .setCustomFieldConfig('fillOpacity', 15)
    .setCustomFieldConfig('lineWidth', 1)
    .setCustomFieldConfig('stacking', { mode: 'normal' } as any)
    .setOption('legend', {
      showLegend: true,
      displayMode: 'list',
      placement: 'bottom',
    } as any)
    .setOverrides((b) => {
      b.matchFieldsWithNameByRegex('.*2xx.*').overrideColor({
        mode: 'fixed',
        fixedColor: 'green',
      } as any);
      b.matchFieldsWithNameByRegex('.*4xx.*').overrideColor({
        mode: 'fixed',
        fixedColor: 'orange',
      } as any);
      b.matchFieldsWithNameByRegex('.*5xx.*').overrideColor({
        mode: 'fixed',
        fixedColor: 'purple',
      } as any);
      // 429 gets special treatment: red line, no stacking, thicker line so it
      // floats above the stacked-area rate curve.
      b.matchFieldsWithNameByRegex('.*429.*')
        .overrideColor({ mode: 'fixed', fixedColor: 'red' } as any)
        .overrideCustomFieldConfig('lineWidth', 3)
        .overrideCustomFieldConfig('fillOpacity', 0)
        .overrideCustomFieldConfig('stacking', { mode: 'none' } as any);
    })
    .build();
}

// Clients panels -------------------------------------------------------------

/**
 * One stat on the clients overview KPI row. Shares the same runner-per-row
 * pattern as the licensing + API usage rows — a single `ClientsOverview` runner
 * feeds all four tiles via `filterFieldsByName`.
 */
function clientsOverviewStat(
  title: string,
  runner: SceneQueryRunner,
  field:
    | 'totalClients'
    | 'usageTotalKb'
    | 'usageDownstreamKb'
    | 'usageUpstreamKb',
  unit?: string
): VizPanel {
  const builder = PanelBuilders.stat()
    .setTitle(title)
    .setData(
      new SceneDataTransformer({
        $data: runner,
        transformations: [
          {
            id: 'filterFieldsByName',
            options: { include: { names: [field] } },
          },
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
 * KPI row for the Clients tab: total unique clients + overall/downstream/
 * upstream usage. All usage values come through the backend in kb — we set
 * the `kbytes` unit so Grafana auto-scales to MB/GB/TB when appropriate.
 */
export function clientsOverviewKpiRow(): VizPanel[] {
  const runner = oneQuery({ kind: QueryKind.ClientsOverview });

  return [
    clientsOverviewStat('Total clients', runner, 'totalClients'),
    clientsOverviewStat('Total usage', runner, 'usageTotalKb', 'kbytes'),
    clientsOverviewStat('Downstream', runner, 'usageDownstreamKb', 'kbytes'),
    clientsOverviewStat('Upstream', runner, 'usageUpstreamKb', 'kbytes'),
  ];
}

/**
 * Top clients by total usage. No drilldown — the clients frame doesn't carry
 * a serial/drilldownUrl column (clients aren't Meraki devices).
 */
export function topClientsTable(): VizPanel {
  return topTable({
    title: 'Top clients',
    description: 'Clients with the highest total usage over the last 24 hours.',
    kind: QueryKind.TopClients,
    hideColumns: ['id'],
  });
}

/**
 * Top devices by total usage, with a drilldown link on the `serial` column
 * backed by the backend-computed `drilldownUrl` field. The drilldownUrl
 * column itself is hidden because it would otherwise show up as a bare URL
 * cell; the link override pulls the value from it.
 */
export function topDevicesTable(): VizPanel {
  return topTable({
    title: 'Top devices',
    description: 'Devices with the highest total usage over the last 24 hours. Click the serial to drill in.',
    kind: QueryKind.TopDevices,
    hideColumns: ['mac', 'drilldownUrl'],
    serialColumn: 'serial',
  });
}

/**
 * Top SSIDs by total usage. No drilldown — the Meraki API doesn't expose a
 * per-SSID detail page that would be useful to link to from here.
 */
export function topSsidsTable(): VizPanel {
  return topTable({
    title: 'Top SSIDs',
    description: 'Wireless SSIDs with the highest total usage over the last 24 hours.',
    kind: QueryKind.TopSsids,
  });
}

/**
 * Top device models by total usage. Summarises the estate by model so estate
 * planning can see which hardware classes are carrying the bulk of traffic.
 */
export function topDeviceModelsTable(): VizPanel {
  return topTable({
    title: 'Top device models',
    description: 'Device models ranked by aggregate usage, with per-model device count.',
    kind: QueryKind.TopDeviceModels,
  });
}

/**
 * Top switches by energy usage (kWh). Drilldown on `serial` via the backend
 * `drilldownUrl` column — same pattern as `topDevicesTable`.
 */
export function topSwitchesByEnergyTable(): VizPanel {
  return topTable({
    title: 'Top switches by energy',
    description: 'Switches ranked by energy consumption (kWh). Click the serial to drill in.',
    kind: QueryKind.TopSwitchesByEnergy,
    hideColumns: ['drilldownUrl'],
    serialColumn: 'serial',
  });
}
