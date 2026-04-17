import { SceneAppPage } from '@grafana/scenes';
import { organizationsScene } from './organizationsScene';
import { organizationDetailScene } from './organizationDetailScene';
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
        return new SceneAppPage({
          title: orgId, // TODO: resolve to org name; needs a state-bound title.
          subTitle: 'Organization detail — networks, devices, and status.',
          titleIcon: 'building',
          url: `${parent.state.url}/${encodeURIComponent(orgId)}`,
          routePath: `${match.params.orgId}/*`,
          getScene: () => organizationDetailScene(orgId),
        });
      },
    },
  ],
});
