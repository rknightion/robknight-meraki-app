import { SceneAppPage } from '@grafana/scenes';
import { configurationScene } from './configurationScene';
import { prefixRoute } from '../../utils/utils.routing';
import { ROUTES } from '../../constants';

/**
 * In-app Configuration page — same form as the classic `/plugins/<id>`
 * config page, but mounted alongside Home / Organizations / Sensors so
 * users don't have to leave the app to tweak settings.
 *
 * `role: "Admin"` on the plugin.json include gates the nav entry for
 * non-admins. The settings API already 403s non-admins server-side, so
 * the gate is purely cosmetic — deep-linking still renders the form.
 */
export const configurationPage = new SceneAppPage({
  title: 'Configuration',
  subTitle: 'Meraki API key, region URL, rate-limit share, and legend label mode.',
  titleIcon: 'cog',
  url: prefixRoute(ROUTES.Configuration),
  routePath: ROUTES.Configuration,
  getScene: () => configurationScene(),
});
