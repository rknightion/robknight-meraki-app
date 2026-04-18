import { SceneAppPage } from '@grafana/scenes';
import { cameraAnalyticsScene } from './cameraAnalyticsScene';
import { cameraOverviewScene } from './cameraOverviewScene';
import { cameraZonesScene } from './cameraZonesScene';

/**
 * Per-camera detail page — a tabbed `SceneAppPage` with three children:
 *   - Overview: top-line device KPIs + onboarding status.
 *   - Analytics: entrances + live occupancy + per-zone history.
 *   - Zones: flat list of every analytics zone configured on the camera.
 *
 * Factory-shaped so the parent drilldown can pass the matched serial plus
 * the parent URL prefix. Mirrors {@link accessPointDetailPage} precisely;
 * Scenes uses the first tab as the default landing page when the user hits
 * the bare detail URL.
 */
export function cameraDetailPage(serial: string, parentUrl: string): SceneAppPage {
  const encodedSerial = encodeURIComponent(serial);
  const baseUrl = `${parentUrl}/${encodedSerial}`;

  return new SceneAppPage({
    title: serial,
    subTitle: 'Camera detail — onboarding status, analytics, and zones.',
    titleIcon: 'camera',
    url: baseUrl,
    routePath: `${encodedSerial}/*`,
    tabs: [
      new SceneAppPage({
        title: 'Overview',
        url: `${baseUrl}/overview`,
        routePath: 'overview',
        getScene: () => cameraOverviewScene(serial),
      }),
      new SceneAppPage({
        title: 'Analytics',
        url: `${baseUrl}/analytics`,
        routePath: 'analytics',
        getScene: () => cameraAnalyticsScene(serial),
      }),
      new SceneAppPage({
        title: 'Zones',
        url: `${baseUrl}/zones`,
        routePath: 'zones',
        getScene: () => cameraZonesScene(serial),
      }),
    ],
  });
}
