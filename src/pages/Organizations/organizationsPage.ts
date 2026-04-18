import { SceneAppPage } from '@grafana/scenes';
import { organizationsScene } from './organizationsScene';
import { organizationDetailPage } from './organizationDetailPage';
import { prefixRoute } from '../../utils/utils.routing';
import { ROUTES } from '../../constants';

export const organizationsPage = new SceneAppPage({
  title: 'Organizations',
  subTitle: 'Meraki organizations visible to the configured API key.',
  url: prefixRoute(ROUTES.Organizations),
  routePath: `${ROUTES.Organizations}/*`,
  getScene: () => organizationsScene(),
  drilldowns: [
    {
      routePath: ':orgId/*',
      getPage: (match, parent) => {
        const orgId = decodeURIComponent(match.params.orgId);
        return organizationDetailPage(orgId, parent.state.url);
      },
    },
  ],
});
