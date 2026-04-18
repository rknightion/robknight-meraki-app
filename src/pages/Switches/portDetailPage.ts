import { SceneAppPage } from '@grafana/scenes';
import { portDetailScene } from './portDetailScene';

/**
 * Per-port detail page — a plain single-scene SceneAppPage mounted at
 * `switches/:serial/ports/:portId`. The parent Ports tab owns the
 * `ports/:portId/*` drilldown and passes both slugs plus the parent URL
 * prefix in, so the page builds a breadcrumb-friendly URL without
 * recomputing the prefix.
 *
 * `routePath` is the port-ID slug alone (no leading `ports/`) because the
 * parent drilldown route already includes `ports/:portId` — the child page
 * only needs to match the remainder, which per Scenes 7 is `:portId`.
 */
export function portDetailPage(
  serial: string,
  portId: string,
  parentUrl: string
): SceneAppPage {
  const encodedPort = encodeURIComponent(portId);
  const baseUrl = `${parentUrl}/${encodedPort}`;
  return new SceneAppPage({
    title: `Port ${portId}`,
    subTitle: `Packet counters and configuration for port ${portId} on ${serial}.`,
    titleIcon: 'sitemap',
    url: baseUrl,
    routePath: encodedPort,
    getScene: () => portDetailScene(serial, portId),
  });
}
