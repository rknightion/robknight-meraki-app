import { SceneAppPage } from '@grafana/scenes';
import { homeScene } from './homeScene';
import { prefixRoute } from '../../utils/utils.routing';
import { ROUTES } from '../../constants';

export const homePage = new SceneAppPage({
  title: 'Home',
  subTitle: 'Overview of your Meraki estate.',
  url: prefixRoute(ROUTES.Home),
  routePath: ROUTES.Home,
  getScene: () => homeScene(),
});
