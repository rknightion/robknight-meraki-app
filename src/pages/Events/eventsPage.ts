import { SceneAppPage } from '@grafana/scenes';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';
import { eventsScene } from './eventsScene';

/**
 * Top-level Events page. Single-scene page (no drilldowns yet — per-event
 * detail pages land in a later phase). The `routePath` uses a `/*` suffix
 * so future drilldowns slot in without reworking the parent.
 */
export const eventsPage = new SceneAppPage({
  title: 'Events',
  subTitle: 'Network events feed with cross-family drilldown.',
  titleIcon: 'list-ul',
  url: `${PLUGIN_BASE_URL}/${ROUTES.Events}`,
  routePath: `${ROUTES.Events}/*`,
  getScene: () => eventsScene(),
});
