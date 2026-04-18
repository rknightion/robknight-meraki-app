import { SceneAppPage } from '@grafana/scenes';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';
import {
  clientSearchScene,
  newClientsScene,
  sessionHistoryScene,
  topTalkersScene,
} from './clientsScene';
import { clientDetailPage } from './clientDetailPage';

/**
 * Top-level Clients page. Tabbed `SceneAppPage` with four children:
 *   - Top Talkers  (default) — `topClients` summary across the org.
 *   - New Clients              — first-seen rows over the selected window.
 *   - Search                   — single-MAC org-wide lookup.
 *   - Session History          — per-client wireless latency timeseries.
 *
 * `routePath: 'clients/*'` (trailing `*`) is required by §1.11 so the
 * `:mac/*` drilldown beneath the parent resolves cleanly.
 */
export const clientsPage = new SceneAppPage({
  title: 'Clients',
  subTitle: 'Top talkers, new clients, MAC search, and per-client session history.',
  titleIcon: 'users-alt',
  url: `${PLUGIN_BASE_URL}/${ROUTES.Clients}`,
  routePath: `${ROUTES.Clients}/*`,
  // Default scene matches the first tab so the bare /clients URL renders
  // something useful even if the tab child lookup hiccups.
  getScene: () => topTalkersScene(),
  tabs: [
    new SceneAppPage({
      title: 'Top talkers',
      url: `${PLUGIN_BASE_URL}/${ROUTES.Clients}/top-talkers`,
      routePath: 'top-talkers',
      getScene: () => topTalkersScene(),
    }),
    new SceneAppPage({
      title: 'New clients',
      url: `${PLUGIN_BASE_URL}/${ROUTES.Clients}/new`,
      routePath: 'new',
      getScene: () => newClientsScene(),
    }),
    new SceneAppPage({
      title: 'Search',
      url: `${PLUGIN_BASE_URL}/${ROUTES.Clients}/search`,
      routePath: 'search',
      getScene: () => clientSearchScene(),
    }),
    new SceneAppPage({
      title: 'Session history',
      url: `${PLUGIN_BASE_URL}/${ROUTES.Clients}/sessions`,
      routePath: 'sessions',
      getScene: () => sessionHistoryScene(),
    }),
  ],
  drilldowns: [
    {
      // `:mac/*` so the per-client detail page's own routePath wildcard
      // can chain extra child routes (none today; future-proofing for a
      // tabbed detail view).
      routePath: ':mac/*',
      getPage: (match, parent) => {
        const mac = decodeURIComponent(match.params.mac);
        return clientDetailPage(mac, parent.state.url);
      },
    },
  ],
});
