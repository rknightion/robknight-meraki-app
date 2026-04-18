import { SceneAppPage } from '@grafana/scenes';
import { cameraAnalyticsScene } from './cameraAnalyticsScene';
import { cameraOverviewScene } from './cameraOverviewScene';
import { cameraBoundariesScene } from './cameraBoundariesScene';

/**
 * Per-camera detail page — a tabbed `SceneAppPage` with three children:
 *   - Overview: top-line device KPIs + onboarding status.
 *   - Analytics: boundary detection counts (in/out per object type).
 *   - Boundaries: flat list of every area + line boundary configured on the
 *     camera.
 *
 * The boundaries model replaced the deprecated `analytics/zones` endpoints in
 * 2024; the Boundaries tab is this detail page's one-stop reference for what
 * the camera is watching.
 */
export function cameraDetailPage(serial: string, parentUrl: string): SceneAppPage {
  const encodedSerial = encodeURIComponent(serial);
  const baseUrl = `${parentUrl}/${encodedSerial}`;

  return new SceneAppPage({
    title: serial,
    subTitle: 'Camera detail — onboarding status, boundary detections, and configured boundaries.',
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
        title: 'Boundaries',
        url: `${baseUrl}/boundaries`,
        routePath: 'boundaries',
        getScene: () => cameraBoundariesScene(serial),
      }),
    ],
  });
}
