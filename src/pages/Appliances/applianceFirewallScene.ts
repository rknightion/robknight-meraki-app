import {
  EmbeddedScene,
  SceneControlsSpacer,
  SceneFlexItem,
  SceneFlexLayout,
  SceneRefreshPicker,
  SceneTimePicker,
  SceneTimeRange,
  SceneVariableSet,
  VariableValueSelectors,
} from '@grafana/scenes';
import { networkVariableForProductTypes } from '../../scene-helpers/variables';
import { applianceSettingsCard, portForwardingTable } from './panels';

/**
 * Per-appliance Firewall tab — port forwarding rules and appliance
 * settings, both scoped to the selected network. The scene exposes a
 * `$network` variable because both backend kinds require a `networkIds`
 * filter (port forwarding + settings are per-network, not per-device),
 * and deriving the network from a serial would need a custom
 * `metricFindQuery` extension we don't want to add right now. The user
 * picks a network on this tab once; both panels populate from the same
 * `$network` interpolation.
 *
 * The `serial` parameter is accepted for API symmetry with the other
 * detail tabs but isn't currently threaded into any runner — see the
 * variables.ts / metricFind TODO for the follow-up that removes the
 * manual network picker.
 */
export function applianceFirewallScene(_serial: string): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-6h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [networkVariableForProductTypes(['appliance'])],
    }),
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
          minHeight: 360,
          body: portForwardingTable('$network'),
        }),
        new SceneFlexItem({
          minHeight: 200,
          body: applianceSettingsCard('$network'),
        }),
      ],
    }),
  });
}
