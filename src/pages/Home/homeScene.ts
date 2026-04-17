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
  makeStatPanel,
  orgDeviceStatusDonut,
  orgInventoryTable,
} from '../../scene-helpers/panels';
import { QueryKind } from '../../datasource/types';

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
        new SceneFlexItem({
          minHeight: 160,
          body: new SceneReactObject({ component: HomeIntro }),
        }),
        new SceneFlexLayout({
          direction: 'row',
          children: [
            new SceneFlexItem({
              height: 200,
              body: makeStatPanel('Organizations', QueryKind.Organizations),
            }),
            new SceneFlexItem({
              height: 200,
              body: orgDeviceStatusDonut('$org'),
            }),
          ],
        }),
        new SceneFlexItem({
          minHeight: 360,
          body: orgInventoryTable(),
        }),
      ],
    }),
  });
}
