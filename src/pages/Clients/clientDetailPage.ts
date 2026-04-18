import { SceneAppPage } from '@grafana/scenes';
import { clientDetailScene } from './clientDetailScene';

/**
 * Per-client drilldown page. Single-scene page (no sub-tabs yet) — the
 * routePath uses a `/*` wildcard so future per-client tabs (e.g. usage
 * timeline, AP history) slot beneath this page without reworking the
 * parent's drilldown match.
 *
 * `parentUrl` is the parent's `/clients` URL; we append the encoded MAC
 * for stable bookmarkable links.
 */
export function clientDetailPage(mac: string, parentUrl: string): SceneAppPage {
  const encodedMac = encodeURIComponent(mac);
  const baseUrl = `${parentUrl}/${encodedMac}`;

  return new SceneAppPage({
    title: mac,
    subTitle: 'Per-client detail — wireless latency history and identity lookup.',
    titleIcon: 'user',
    url: baseUrl,
    routePath: `${encodedMac}/*`,
    getScene: () => clientDetailScene(mac),
  });
}
