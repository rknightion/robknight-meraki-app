import {
  PanelBuilders,
  SceneQueryRunner,
  VizPanel,
} from '@grafana/scenes';
import { MERAKI_DS_REF } from '../../scene-helpers/datasource';
import { QueryKind } from '../../datasource/types';

// Local copy of the `oneQuery` helper that other v0.5 page directories
// already inline. We deliberately don't import the shared
// `src/scene-helpers/panels.ts` factory because it doesn't expose
// `oneQuery` and we want this directory to be self-contained — adding a
// shared export specifically for Topology would couple two unrelated
// pages.
function oneQuery(params: {
  refId?: string;
  kind: QueryKind;
  orgId?: string;
  networkIds?: string[];
  serials?: string[];
}): SceneQueryRunner {
  const { refId = 'A', kind, orgId, networkIds, serials } = params;
  const query: Record<string, unknown> & { refId: string } = { refId, kind };
  query.orgId = orgId ?? '$org';
  if (networkIds && networkIds.length > 0) {
    query.networkIds = networkIds;
  }
  if (serials && serials.length > 0) {
    query.serials = serials;
  }
  return new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [query],
  });
}

/**
 * Row 1 — org-wide network geomap.
 *
 * Backed by the `networkGeo` query kind (one wide table frame:
 * networkId, name, lat, lng). Coordinates are derived server-side from
 * the centroid of every device in each network — Meraki's networks
 * endpoint does not carry geo, but the devices feed does (and we
 * already cache it for 5 m).
 *
 * The Geomap viz auto-detects lat/lng/name fields by name. Networks
 * with no geo-tagged devices are dropped server-side and counted in a
 * `data.Notice` attached to the frame, so operators see "X networks
 * lack coordinates" surfaced as a panel-level banner.
 */
export function networkGeomapPanel(): VizPanel {
  const runner = oneQuery({ kind: QueryKind.NetworkGeo });
  return PanelBuilders.geomap()
    .setTitle('Network locations')
    .setDescription(
      'One marker per network, positioned at the centroid of its geo-tagged ' +
        'devices. Set device locations in Meraki Dashboard to populate.'
    )
    .setData(runner)
    .setNoValue('No networks have geo-tagged devices yet.')
    // Explicit markers layer — PanelBuilders.geomap() ships no data layers
    // by default, so without this the map renders but no points appear.
    // `mode: 'coords'` points the viz at the `lat`/`lng` numeric fields
    // the networkGeo handler emits; `name` is what shows in the layer list.
    .setOption('layers', [
      {
        type: 'markers',
        name: 'Networks',
        config: {},
        location: {
          mode: 'coords',
          latitude: 'lat',
          longitude: 'lng',
        },
        tooltip: true,
      },
    ] as any)
    // Auto-fit the view around the marker layer so an operator with
    // networks spread across continents isn't stuck at the zero meridian.
    .setOption('view', { id: 'fit', allLayers: true, padding: 5 } as any)
    .build();
}

/**
 * Row 2 — per-network device link graph.
 *
 * Backed by the `deviceLldpCdp` query kind which emits the two-frame
 * Grafana Node Graph contract:
 *   - "nodes" frame: id, title, subtitle, mainstat
 *   - "edges" frame: id, source, target
 *
 * §4.4.4-D explicitly gates the LLDP/CDP fan-out to per-network scope —
 * the panel binds to the single-select `$network` variable. The backend
 * caps the per-network device fan-out at 50 devices (see
 * `deviceLldpCdpFanoutCap` in pkg/plugin/query/topology.go) so a
 * 200-device network won't blow through the rate budget.
 */
export function networkLinkGraphPanel(): VizPanel {
  const runner = oneQuery({
    kind: QueryKind.DeviceLldpCdp,
    networkIds: ['$network'],
  });
  return PanelBuilders.nodegraph()
    .setTitle('Device link graph (LLDP/CDP)')
    .setDescription(
      'Per-network LLDP/CDP topology. Nodes are Meraki devices in the ' +
        'selected network; edges are discovered via LLDP or CDP. External ' +
        'neighbours (e.g. upstream routers) appear as nodes labelled ' +
        '"external".'
    )
    .setData(runner)
    .setNoValue('No LLDP/CDP neighbours discovered for this network.')
    .build();
}
