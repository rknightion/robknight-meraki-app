import { SceneAppPage } from '@grafana/scenes';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';
import { trafficScene } from './trafficScene';

/**
 * Top-level Traffic Analytics page — L7 application + category breakdown
 * across the selected organisation, with a per-network traffic-analysis
 * guard banner.
 *
 * `routePath` carries the trailing `/*` even though there is no drilldown
 * yet: future per-application detail pages can slot in as `drilldowns:
 * [...]` without re-working the parent route.
 */
export const trafficPage = new SceneAppPage({
  title: 'Traffic',
  subTitle:
    'Layer-7 application and category usage across your Meraki organisation, plus a per-network traffic mix.',
  titleIcon: 'chart-line',
  url: `${PLUGIN_BASE_URL}/${ROUTES.Traffic}`,
  routePath: `${ROUTES.Traffic}/*`,
  getScene: () => trafficScene(),
});
