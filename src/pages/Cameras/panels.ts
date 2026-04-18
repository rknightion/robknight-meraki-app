import { FieldColorModeId } from '@grafana/schema';
import {
  PanelBuilders,
  SceneDataTransformer,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { deviceAvailabilityStat } from '../../scene-helpers/panels';
import { QueryKind } from '../../datasource/types';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';
import type { MerakiProductType } from '../../types';

// Shared query-runner factory -------------------------------------------------
//
// Local copy of the `oneQuery` helper used across per-area panels.ts files so
// the Cameras area doesn't depend on an internal helper in the shared panels
// module (which is being mutated in parallel during Wave 3). Mirrors the
// Access Points / Switches copies precisely.

interface QueryParams {
  refId?: string;
  kind: QueryKind;
  orgId?: string;
  networkIds?: string[];
  serials?: string[];
  productTypes?: MerakiProductType[];
  metrics?: string[];
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

// Camera KPI row -------------------------------------------------------------

/**
 * KPI row for the Cameras overview: three stat panels with counts of MV
 * devices in each Meraki-reported status bucket. Server-side aggregation
 * via `DeviceAvailabilityCounts` (todos.txt §G.20).
 */
export function cameraStatusKpiRow(): VizPanel[] {
  const productTypes: MerakiProductType[] = ['camera'];
  return [
    deviceAvailabilityStat({
      title: 'Cameras online',
      fieldName: 'online',
      productTypes,
      thresholds: [
        { value: 0, color: 'red' },
        { value: 1, color: 'green' },
      ],
    }),
    deviceAvailabilityStat({
      title: 'Cameras alerting',
      fieldName: 'alerting',
      productTypes,
      thresholds: [
        { value: 0, color: 'green' },
        { value: 1, color: 'orange' },
      ],
    }),
    deviceAvailabilityStat({
      title: 'Cameras offline',
      fieldName: 'offline',
      productTypes,
      thresholds: [
        { value: 0, color: 'green' },
        { value: 1, color: 'red' },
      ],
    }),
  ];
}

// Onboarding + inventory tables ---------------------------------------------

/**
 * Camera onboarding status table — one row per camera. Drilldown link on the
 * `serial` column uses the backend-emitted `drilldownUrl` column so rows
 * route to the per-camera detail page. The backend always sets productType
 * to "camera" for this kind, so the URL is always `/cameras/<serial>`.
 *
 * Status column has a value-mapping that colours the rows green for a
 * complete/connected onboard, orange for incomplete, red for unboxed gear.
 */
export function cameraOnboardingTable(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.CameraOnboarding,
  });
  return PanelBuilders.table()
    .setTitle('Onboarding status')
    .setDescription('Camera onboarding state per device in the selected organization.')
    .setData(hideColumns(runner, ['drilldownUrl']))
    .setNoValue('No cameras reporting onboarding state for this organization.')
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideLinks([
        {
          title: 'Open camera',
          url: '${__data.fields.drilldownUrl}',
        },
      ]);
      b.matchFieldsWithName('status').overrideMappings([
        {
          type: 'value' as any,
          options: {
            complete: { color: 'green', index: 0, text: 'Complete' },
            connected: { color: 'green', index: 1, text: 'Connected' },
            incomplete: { color: 'orange', index: 2, text: 'Incomplete' },
            unboxed: { color: 'red', index: 3, text: 'Unboxed' },
          },
        },
      ]);
      b.matchFieldsWithName('status').overrideCustomFieldConfig('cellOptions', {
        type: 'color-background',
      } as any);
      b.matchFieldsWithName('updatedAt').overrideCustomFieldConfig('width', 200);
    })
    .build();
}

/**
 * Table of every MV device in the selected org. Serial column drills into
 * the per-camera detail scene; mac/lat/lng columns are hidden because
 * they're rarely useful in a dense inventory view. Uses the Devices-emitted
 * `drilldownUrl` column so future product-type renames only touch the
 * backend.
 */
export function cameraInventoryTable(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.Devices,
    productTypes: ['camera'],
  });
  return PanelBuilders.table()
    .setTitle('Camera inventory')
    .setDescription('MV camera devices in the selected organization. Click a serial to drill in.')
    .setData(hideColumns(runner, ['lat', 'lng', 'mac', 'drilldownUrl']))
    .setNoValue('No camera devices found in this organization.')
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideLinks([
        {
          title: 'Open camera',
          url: `${PLUGIN_BASE_URL}/${ROUTES.Cameras}/\${__value.raw:percentencode}?var-org=\${var-org:queryparam}`,
        },
      ]);
      b.matchFieldsWithName('firmware').overrideCustomFieldConfig('width', 140);
      b.matchFieldsWithName('address').overrideCustomFieldConfig('width', 260);
    })
    .build();
}

// Per-camera analytics panels ------------------------------------------------

/**
 * Entrances-over-time timeseries for a single camera, aggregated across every
 * zone by default. The `$objectType` variable threads through `q.Metrics[0]`
 * so users can flip between person and vehicle counts without re-editing the
 * panel. One frame per `(serial, zoneId)` — `DisplayNameFromDS` is already
 * baked by the backend so the legend is clean.
 */
export function cameraEntrancesTimeseries(serial: string): VizPanel {
  return PanelBuilders.timeseries()
    .setTitle('Zone entrances')
    .setDescription('Per-zone entrance counts for this camera, in the selected object-type.')
    .setData(
      oneQuery({
        kind: QueryKind.CameraAnalyticsOverview,
        serials: [serial],
        metrics: ['$objectType'],
        maxDataPoints: 500,
      })
    )
    .setNoValue('No analytics data in the selected range.')
    .setColor({ mode: FieldColorModeId.PaletteClassic })
    .setCustomFieldConfig('lineWidth', 1)
    .setCustomFieldConfig('fillOpacity', 15)
    .setCustomFieldConfig('spanNulls', true)
    .setCustomFieldConfig('showPoints', 'auto' as any)
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .setOption('tooltip', { mode: 'multi', sort: 'desc' } as any)
    .build();
}

/**
 * Live occupancy table — a wide snapshot of the current per-zone person /
 * vehicle counts for one camera. Column order matches the backend frame
 * shape (`serial, ts, zone_id, person, vehicle`).
 */
export function cameraLiveOccupancyTable(serial: string): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.CameraAnalyticsLive,
    serials: [serial],
  });
  return PanelBuilders.table()
    .setTitle('Live occupancy')
    .setDescription('Current person / vehicle counts per zone, refreshed near-live.')
    .setData(runner)
    .setNoValue('No live occupancy data reported by this camera.')
    .setOverrides((b) => {
      b.matchFieldsWithName('ts').overrideCustomFieldConfig('width', 200);
      b.matchFieldsWithName('zone_id').overrideCustomFieldConfig('width', 120);
      b.matchFieldsWithName('person').overrideUnit('short');
      b.matchFieldsWithName('vehicle').overrideUnit('short');
    })
    .build();
}

/**
 * Per-camera zones table — one row per configured zone with its type and
 * display label. Primarily used on the Zones tab; the `$zone` variable is
 * hydrated from the same backend kind via metricFind.
 */
export function cameraZonesTable(serial: string): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.CameraAnalyticsZones,
    serials: [serial],
  });
  return PanelBuilders.table()
    .setTitle('Zones')
    .setDescription('Analytics zones configured on this camera.')
    .setData(runner)
    .setNoValue('No analytics zones configured on this camera.')
    .setOverrides((b) => {
      b.matchFieldsWithName('zoneId').overrideCustomFieldConfig('width', 120);
      b.matchFieldsWithName('type').overrideCustomFieldConfig('width', 140);
    })
    .build();
}

/**
 * Entrances-over-time timeseries for one (serial, zone) pair. The zone id
 * rides through `q.Metrics[0]` (backend contract) and object-type overrides
 * via `q.Metrics[1]`. Rendered as a single-series chart — the backend emits
 * exactly one frame per request.
 */
export function cameraZoneHistoryTimeseries(serial: string): VizPanel {
  return PanelBuilders.timeseries()
    .setTitle('Zone history')
    .setDescription('Entrance counts for the selected zone and object type.')
    .setData(
      oneQuery({
        kind: QueryKind.CameraAnalyticsZoneHistory,
        serials: [serial],
        // Backend overloads: metrics[0] = zoneId, metrics[1] = objectType. See
        // `pkg/plugin/query/camera.go::handleCameraAnalyticsZoneHistory` for
        // the contract.
        metrics: ['$zone', '$objectType'],
        maxDataPoints: 500,
      })
    )
    .setNoValue('No zone-history data in the selected range.')
    .setColor({ mode: FieldColorModeId.PaletteClassic })
    .setCustomFieldConfig('lineWidth', 2)
    .setCustomFieldConfig('fillOpacity', 15)
    .setCustomFieldConfig('spanNulls', true)
    .setCustomFieldConfig('showPoints', 'auto' as any)
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .setOption('tooltip', { mode: 'single' } as any)
    .build();
}

/**
 * Retention-profile table scoped to the current `$network`. Surfaces the
 * configured recording behaviour (default profile, audio, motion-based,
 * bandwidth caps, max retention) so operators can audit what's captured
 * without bouncing into the Meraki dashboard.
 */
export function cameraRetentionProfilesPanel(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.CameraRetentionProfiles,
    networkIds: ['$network'],
  });
  return PanelBuilders.table()
    .setTitle('Retention profiles')
    .setDescription('Recording retention profiles configured for the selected network(s).')
    .setData(runner)
    .setNoValue('No retention profiles configured for these networks.')
    .setOverrides((b) => {
      b.matchFieldsWithName('maxRetentionDays').overrideUnit('d');
      b.matchFieldsWithName('name').overrideCustomFieldConfig('width', 200);
    })
    .build();
}

// Per-camera overview KPIs --------------------------------------------------

/**
 * KPI tiles for one camera — status, model, firmware, network. Mirrors the
 * AP detail KPI row exactly: each tile is a stat panel that filters the
 * single-row Devices frame (or the availability frame) down to one column
 * via a `filterFieldsByName` transform so the stat viz picks up exactly one
 * value. Status has a background colour-mapping keyed on the availability
 * bucket.
 */
export function cameraOverviewKpiRow(serial: string): VizPanel[] {
  const deviceRunner = oneQuery({
    kind: QueryKind.Devices,
    serials: [serial],
    productTypes: ['camera'],
  });

  function pickFromDevices(title: string, field: string): VizPanel {
    return PanelBuilders.stat()
      .setTitle(title)
      .setData(
        new SceneDataTransformer({
          $data: deviceRunner,
          transformations: [
            {
              id: 'filterFieldsByName',
              options: { include: { names: [field] } },
            },
          ],
        })
      )
      .setNoValue('—')
      .setOption('reduceOptions', {
        values: false,
        calcs: ['lastNotNull'],
        // String fields need the wildcard regex — `fields: ''` means numeric
        // only, which silently hides model/firmware/networkId strings.
        fields: '/.*/',
      } as any)
      .setOption('colorMode', 'none' as any)
      .build();
  }

  const availabilityRunner = oneQuery({
    kind: QueryKind.DeviceAvailabilities,
    serials: [serial],
    productTypes: ['camera'],
  });

  const status = PanelBuilders.stat()
    .setTitle('Status')
    .setData(
      new SceneDataTransformer({
        $data: availabilityRunner,
        transformations: [
          {
            id: 'filterFieldsByName',
            options: { include: { names: ['status'] } },
          },
        ],
      })
    )
    .setNoValue('unknown')
    .setOption('reduceOptions', {
      values: false,
      calcs: ['lastNotNull'],
      fields: '/.*/',
    } as any)
    .setOption('colorMode', 'background' as any)
    .setMappings([
      {
        type: 'value' as any,
        options: {
          online: { color: 'green', index: 0, text: 'Online' },
          alerting: { color: 'orange', index: 1, text: 'Alerting' },
          offline: { color: 'red', index: 2, text: 'Offline' },
          dormant: { color: 'blue', index: 3, text: 'Dormant' },
        },
      },
    ])
    .build();

  return [
    status,
    pickFromDevices('Model', 'model'),
    pickFromDevices('Firmware', 'firmware'),
    pickFromDevices('Network', 'networkId'),
  ];
}
