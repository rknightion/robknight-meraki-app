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
// module.

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
 * they're rarely useful in a dense inventory view.
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
          url: `${PLUGIN_BASE_URL}/${ROUTES.Cameras}/\${__value.raw:percentencode}\${__url.params}`,
        },
      ]);
      b.matchFieldsWithName('firmware').overrideCustomFieldConfig('width', 140);
      b.matchFieldsWithName('address').overrideCustomFieldConfig('width', 260);
    })
    .build();
}

// Per-camera boundaries panels ----------------------------------------------

/**
 * Per-camera boundaries table — one row per configured area boundary plus
 * one row per configured line boundary for the given serial. Uses the two
 * dedicated `CameraBoundary{Areas,Lines}` kinds and merges the frames with
 * a `merge` transform so the resulting table renders both kinds with a
 * `kind` column that callers can colour on.
 */
export function cameraBoundariesTable(serial: string): VizPanel {
  const areasRunner = oneQuery({
    refId: 'A',
    kind: QueryKind.CameraBoundaryAreas,
    serials: [serial],
  });
  const linesRunner = oneQuery({
    refId: 'B',
    kind: QueryKind.CameraBoundaryLines,
    serials: [serial],
  });
  // Nest both runners so Scenes can merge their frames. We wrap one runner
  // inside a second that references both query-refs via `$data` — Grafana's
  // `merge` transform concatenates frames with a shared schema, which the
  // backend guarantees here (same column set for areas + lines).
  const merged = new SceneDataTransformer({
    $data: new SceneQueryRunner({
      datasource: MERAKI_DS_REF,
      queries: [
        ...(areasRunner.state.queries as any[]),
        ...(linesRunner.state.queries as any[]),
      ],
    }),
    transformations: [
      { id: 'merge', options: {} },
      {
        id: 'organize',
        options: {
          excludeByName: { directionVertex_x: true, directionVertex_y: true },
          renameByName: {},
        },
      },
    ],
  });
  return PanelBuilders.table()
    .setTitle('Boundaries')
    .setDescription('Area and line boundaries configured on this camera. In/out detection counts appear on the Analytics tab.')
    .setData(merged)
    .setNoValue('No boundaries configured for this camera. Add area or line crossings in the Meraki Dashboard to populate detections.')
    .setOverrides((b) => {
      b.matchFieldsWithName('kind').overrideMappings([
        {
          type: 'value' as any,
          options: {
            area: { color: 'blue', index: 0, text: 'Area' },
            line: { color: 'purple', index: 1, text: 'Line' },
          },
        },
      ]);
      b.matchFieldsWithName('kind').overrideCustomFieldConfig('cellOptions', {
        type: 'color-background',
      } as any);
      b.matchFieldsWithName('kind').overrideCustomFieldConfig('width', 80);
      b.matchFieldsWithName('name').overrideCustomFieldConfig('width', 200);
      b.matchFieldsWithName('boundaryId').overrideCustomFieldConfig('width', 180);
    })
    .build();
}

/**
 * Detection-count timeseries scoped to one camera. The backend resolves the
 * camera's boundaries (both areas and lines) from the serial, then calls the
 * org-level `/camera/detections/history/byBoundary/byInterval` endpoint
 * exactly once. Output is one frame per (boundaryId × objectType ×
 * direction), stacked naturally by the timeseries viz when the user picks
 * stacking.
 *
 * `$objectType` (person / vehicle) is threaded via `metrics[1]` so the same
 * panel can flip between object types without re-querying the boundary list.
 */
export function cameraDetectionsTimeseries(serial: string): VizPanel {
  return PanelBuilders.timeseries()
    .setTitle('Boundary detections')
    .setDescription('In/out detection counts per configured boundary for the selected object type.')
    .setData(
      oneQuery({
        kind: QueryKind.CameraDetectionsHistory,
        serials: [serial],
        // metrics[0] left blank so the backend resolves boundaries from the
        // serial; metrics[1] selects the object type.
        metrics: ['', '$objectType'],
        maxDataPoints: 500,
      })
    )
    .setNoValue('No detection data in the selected range. Ensure boundaries are configured for this camera.')
    .setColor({ mode: FieldColorModeId.PaletteClassic })
    .setCustomFieldConfig('lineWidth', 2)
    .setCustomFieldConfig('fillOpacity', 15)
    .setCustomFieldConfig('spanNulls', true)
    .setCustomFieldConfig('showPoints', 'auto' as any)
    .setOption('legend', { showLegend: true, displayMode: 'list', placement: 'bottom' } as any)
    .setOption('tooltip', { mode: 'multi', sort: 'desc' } as any)
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
 * AP detail KPI row exactly.
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
