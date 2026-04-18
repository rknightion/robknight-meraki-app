import {
  EmbeddedScene,
  PanelBuilders,
  SceneControlsSpacer,
  SceneDataTransformer,
  SceneFlexItem,
  SceneFlexLayout,
  SceneQueryRunner,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
  VizPanel,
} from '@grafana/scenes';
import { orgDevicesTable } from '../../scene-helpers/panels';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';

/**
 * Devices tab for a single organization. Pairs the current-state inventory table
 * (top) with a live availability-change feed (bottom). The two panels answer
 * different questions: "what's connected right now?" vs. "who's flapping?".
 *
 * The change-history panel is backed by the §7.3-D query kind
 * `DeviceAvailabilityChanges`; it respects the scene time range so users can
 * widen the window to 7d / 30d for trend investigations. Cross-family drilldown
 * is handled by the backend's computed `drilldownUrl` column (§1.12).
 */
export function organizationDevicesScene(orgId: string): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-24h', to: 'now' }),
    controls: [
      new SceneControlsSpacer(),
      new SceneTimePicker({ isOnCanvas: true }),
      new SceneRefreshPicker({ intervals: ['30s', '1m', '5m'], isOnCanvas: true }),
    ],
    body: new SceneFlexLayout({
      direction: 'column',
      children: [
        new SceneFlexItem({
          minHeight: 420,
          body: orgDevicesTable(orgId),
        }),
        new SceneFlexItem({
          minHeight: 360,
          body: availabilityChangesTable(orgId),
        }),
      ],
    }),
  });
}

// availabilityChangesTable renders the org-wide device-availability change history
// (online ↔ offline flaps) for the current panel time range. Kept local to the
// Organizations area because no other scene needs it today; promote to
// scene-helpers/panels.ts if a second call site lands.
function availabilityChangesTable(orgId: string): VizPanel {
  const runner = new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [
      {
        refId: 'A',
        kind: QueryKind.DeviceAvailabilityChanges,
        orgId,
      },
    ],
  });
  // drilldownUrl / model stay hidden — drilldownUrl backs the per-row link and
  // model is rarely the primary filter on a flaps list.
  const organized = new SceneDataTransformer({
    $data: runner,
    transformations: [
      {
        id: 'organize',
        options: {
          excludeByName: {
            drilldownUrl: true,
            model: true,
          },
          renameByName: {},
        },
      },
    ],
  });
  return PanelBuilders.table()
    .setTitle('Device availability changes')
    .setDescription('State transitions (online ↔ offline) over the selected time range. Updates every ~60s on the Meraki side.')
    .setData(organized)
    .setNoValue('No device availability changes in the selected range.')
    .setOverrides((b) => {
      b.matchFieldsWithName('serial').overrideLinks([
        {
          title: 'Open device',
          url: '${__data.fields.drilldownUrl}',
        },
      ]);
      b.matchFieldsWithName('ts').overrideCustomFieldConfig('width', 180);
      b.matchFieldsWithName('oldStatus').overrideCustomFieldConfig('width', 110);
      b.matchFieldsWithName('newStatus').overrideCustomFieldConfig('width', 110);
    })
    .build();
}
