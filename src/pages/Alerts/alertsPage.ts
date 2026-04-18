import { SceneAppPage } from '@grafana/scenes';
import { alertsScene } from './alertsScene';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';

/**
 * Top-level Alerts page — assurance alerts across the selected
 * organization, plus a severity filter and a 24h-default time picker.
 *
 * `routePath` uses a `/*` suffix even though there is no drilldown yet.
 * That future-proofs the area: when per-alert detail pages land, they
 * can slot in as `drilldowns: [{ routePath: ':id/*', ... }]` without
 * re-working the parent.
 */
export const alertsPage = new SceneAppPage({
  title: 'Alerts',
  subTitle: 'Assurance alerts across your Meraki organizations.',
  titleIcon: 'bell',
  url: `${PLUGIN_BASE_URL}/${ROUTES.Alerts}`,
  routePath: `${ROUTES.Alerts}/*`,
  getScene: () => alertsScene(),
});
