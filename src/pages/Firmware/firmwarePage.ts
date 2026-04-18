import { SceneAppPage } from '@grafana/scenes';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';
import { firmwareScene } from './firmwareScene';

/**
 * Top-level Firmware & Lifecycle page (v0.5 §4.4.4-B). Single-scene page —
 * the `routePath` uses a `/*` suffix so future drilldowns (e.g. per-device
 * firmware history) can slot in without reworking the parent.
 */
export const firmwarePage = new SceneAppPage({
  title: 'Firmware & Lifecycle',
  subTitle:
    'Per-device firmware versions, pending upgrades, upgrade windows, and ' +
    'end-of-sale / end-of-support tracking.',
  titleIcon: 'rocket',
  url: `${PLUGIN_BASE_URL}/${ROUTES.Firmware}`,
  routePath: `${ROUTES.Firmware}/*`,
  getScene: () => firmwareScene(),
});
