import { SceneAppPage } from '@grafana/scenes';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';
import { apiUsageScene } from './apiUsageScene';
import { clientsScene } from './clientsScene';
import { licensingScene } from './licensingScene';

/**
 * Insights page — tabbed `SceneAppPage` with Licensing (default) / API Usage /
 * Clients. Same shape as `organizationDetailPage`: each tab is its own
 * `SceneAppPage` child with a `routePath` that slots beneath the parent
 * wildcard. Scenes uses the first tab as the default landing page when the
 * user hits the bare `/insights` URL.
 *
 * `getScene` on the parent also returns `licensingScene()` so the bare URL
 * always has a valid scene to render even if the tab child lookup hiccups.
 */
export const insightsPage = new SceneAppPage({
  title: 'Insights',
  subTitle: 'Licensing health, API usage, and client activity trends.',
  titleIcon: 'graph-bar',
  url: `${PLUGIN_BASE_URL}/${ROUTES.Insights}`,
  // Wildcard so the tab children (licensing / api-usage / clients) route
  // correctly beneath this page.
  routePath: `${ROUTES.Insights}/*`,
  // Default scene redirects to licensing (first tab) — matches the
  // organizationDetailPage convention.
  getScene: () => licensingScene(),
  tabs: [
    new SceneAppPage({
      title: 'Licensing',
      url: `${PLUGIN_BASE_URL}/${ROUTES.Insights}/licensing`,
      routePath: 'licensing',
      getScene: () => licensingScene(),
    }),
    new SceneAppPage({
      title: 'API Usage',
      url: `${PLUGIN_BASE_URL}/${ROUTES.Insights}/api-usage`,
      routePath: 'api-usage',
      getScene: () => apiUsageScene(),
    }),
    new SceneAppPage({
      title: 'Clients',
      url: `${PLUGIN_BASE_URL}/${ROUTES.Insights}/clients`,
      routePath: 'clients',
      getScene: () => clientsScene(),
    }),
  ],
});
