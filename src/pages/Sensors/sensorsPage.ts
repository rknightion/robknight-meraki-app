import { SceneAppPage } from '@grafana/scenes';
import { sensorsScene } from './sensorsScene';
import { sensorDetailScene } from './sensorDetailScene';
import { prefixRoute } from '../../utils/utils.routing';
import { ROUTES } from '../../constants';
import { familyGateWrap } from '../../scene-helpers/familyGate';

export const sensorsPage = new SceneAppPage({
  title: 'Sensors',
  subTitle:
    'Environmental sensor (MT) readings — temperature, humidity, air quality, door, water, and more.',
  url: prefixRoute(ROUTES.Sensors),
  routePath: `${ROUTES.Sensors}/*`,
  getScene: familyGateWrap('sensor', () => sensorsScene()),
  drilldowns: [
    {
      routePath: ':serial/*',
      getPage: (match, parent) => {
        const serial = decodeURIComponent(match.params.serial);
        return new SceneAppPage({
          title: serial,
          subTitle: 'Sensor detail — all metrics reported by this device.',
          titleIcon: 'cube',
          url: `${parent.state.url}/${encodeURIComponent(serial)}`,
          routePath: `${match.params.serial}/*`,
          getScene: () => sensorDetailScene(serial),
        });
      },
    },
  ],
});
