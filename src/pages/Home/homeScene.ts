import {
  EmbeddedScene,
  SceneControlsSpacer,
  SceneFlexItem,
  SceneFlexLayout,
  SceneReactObject,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
  SceneVariableSet,
  VariableValueSelectors,
} from '@grafana/scenes';
import { HomeIntro } from './HomeIntro';
import { orgVariable } from '../../scene-helpers/variables';
import {
  orgDeviceStatusDonut,
  orgInventoryTable,
  organizationsCountStat,
} from '../../scene-helpers/panels';
import { configGuardFlexItem } from '../../scene-helpers/ConfigGuard';
import { recentAlertsTile } from '../Alerts/panels';

export function homeScene() {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
    $variables: new SceneVariableSet({ variables: [orgVariable()] }),
    controls: [
      new VariableValueSelectors({}),
      new SceneControlsSpacer(),
      new SceneTimePicker({ isOnCanvas: true }),
      new SceneRefreshPicker({ intervals: ['30s', '1m', '5m'], isOnCanvas: true }),
    ],
    body: new SceneFlexLayout({
      direction: 'column',
      children: [
        configGuardFlexItem(),
        new SceneFlexItem({
          minHeight: 160,
          body: new SceneReactObject({ component: HomeIntro }),
        }),
        new SceneFlexLayout({
          direction: 'row',
          children: [
            new SceneFlexItem({
              height: 200,
              body: organizationsCountStat(),
            }),
            new SceneFlexItem({
              height: 200,
              body: orgDeviceStatusDonut('$org'),
            }),
          ],
        }),
        // Recent alerts tile — sits above the org inventory so operators
        // land on "what's firing?" before they scan the inventory table.
        // The tile uses the Alerts query runner with a `limit` transform
        // to show the top five newest alerts across the selected org.
        new SceneFlexItem({
          minHeight: 260,
          body: recentAlertsTile('$org'),
        }),
        new SceneFlexItem({
          minHeight: 360,
          body: orgInventoryTable(),
        }),
      ],
    }),
  });
}
