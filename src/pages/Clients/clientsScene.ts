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
import { networkVariable, orgVariable } from '../../scene-helpers/variables';
import { configGuardFlexItem } from '../../scene-helpers/ConfigGuard';
import {
  clientSearchTable,
  newClientsTable,
  sessionHistoryTimeseries,
  topTalkersTable,
} from './panels';
import { clientSearchVariable } from './variables';

/**
 * Shared layout shell for every Clients tab. Each tab provides its own body
 * children; the wrapper handles variables / config-guard banner / time
 * picker / refresh picker so the four tabs feel cohesive without
 * duplicating boilerplate.
 *
 * Variables on every tab:
 *  - $org, $network — standard org / network cascade.
 *  - $client       — free-form MAC. Empty string is "no client selected".
 *
 * Time range default: 24h. The Top Talkers tab uses a server-side fixed
 * 24h lookback regardless of the picker; everything else honours the
 * picker.
 */
function clientsTabShell(bodyChildren: SceneFlexItem[]): EmbeddedScene {
  return new EmbeddedScene({
    $timeRange: new SceneTimeRange({ from: 'now-24h', to: 'now' }),
    $variables: new SceneVariableSet({
      variables: [orgVariable(), networkVariable(), clientSearchVariable()],
    }),
    controls: [
      new VariableValueSelectors({}),
      new SceneControlsSpacer(),
      new SceneTimePicker({ isOnCanvas: true }),
      new SceneRefreshPicker({ intervals: ['1m', '5m', '15m'], isOnCanvas: true }),
    ],
    body: new SceneFlexLayout({
      direction: 'column',
      children: [configGuardFlexItem(), ...bodyChildren],
    }),
  });
}

/**
 * Top Talkers tab — full-width table backed by `topClients`.
 */
export function topTalkersScene(): EmbeddedScene {
  return clientsTabShell([
    new SceneFlexItem({ minHeight: 520, body: topTalkersTable() }),
  ]);
}

/**
 * New Clients tab — table of clients first observed in the selected window
 * across the chosen networks.
 */
export function newClientsScene(): EmbeddedScene {
  return clientsTabShell([
    new SceneFlexItem({ minHeight: 520, body: newClientsTable() }),
  ]);
}

/**
 * Search tab — single-MAC lookup table. The panel's NoValue text + the
 * backend's empty-state notice cover both "no input yet" and "MAC not
 * found" cases.
 */
export function clientSearchScene(): EmbeddedScene {
  return clientsTabShell([
    new SceneFlexItem({ minHeight: 360, body: clientSearchTable() }),
  ]);
}

/**
 * Session History tab — per-client latency timeseries. Requires both a
 * network selection and a `$client` MAC; until both are picked the panel
 * renders the friendly "Pick a client and a wireless network" hint.
 */
export function sessionHistoryScene(): EmbeddedScene {
  return clientsTabShell([
    new SceneFlexItem({ minHeight: 480, body: sessionHistoryTimeseries() }),
  ]);
}
