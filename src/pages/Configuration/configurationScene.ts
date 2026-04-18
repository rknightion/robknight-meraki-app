import React from 'react';
import {
  EmbeddedScene,
  SceneFlexItem,
  SceneFlexLayout,
  SceneReactObject,
} from '@grafana/scenes';
import { ConfigurationPanel } from './ConfigurationPanel';

/**
 * Scene wrapper for the in-app Configuration page. Renders the shared
 * MerakiConfigForm via a React object so the form state lives in React
 * (not scene state) — saving reloads the whole page anyway, which resets
 * both trees together.
 */
export function configurationScene() {
  return new EmbeddedScene({
    body: new SceneFlexLayout({
      direction: 'column',
      children: [
        new SceneFlexItem({
          body: new SceneReactObject({
            component: () => React.createElement(ConfigurationPanel),
          }),
        }),
      ],
    }),
  });
}
