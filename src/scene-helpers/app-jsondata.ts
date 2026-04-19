import { config } from '@grafana/runtime';

import { PLUGIN_ID } from '../constants';
import type { AppJsonData } from '../types';

/**
 * Scene-build-time read of the plugin's `jsonData` slice. Returns
 * `undefined` if the plugin isn't registered yet (shouldn't happen in
 * practice, but the surface tolerates it cleanly). Uses a type assertion
 * against `config.apps` because Grafana's public `AppPluginConfig` type
 * doesn't expose `jsonData` directly even though the runtime value has
 * always carried it — the `config.apps` API itself is also flagged as
 * deprecated by Grafana but there is no documented replacement for
 * scene-factory reads at build time (React hooks can't be called
 * outside a component).
 *
 * Scene factories flip once at build time — operators toggling
 * `jsonData.recordings.targetDatasourceUid` or per-rule enable state
 * see the new panel only after the `window.location.reload()` that the
 * plugin's config save path already performs. See root `CLAUDE.md`
 * gotcha G.15 for the rationale behind the hard reload.
 */
export function readAppJsonData(): AppJsonData | undefined {
  const apps = (config as unknown as { apps?: Record<string, { jsonData?: unknown }> }).apps;
  const jd = apps?.[PLUGIN_ID]?.jsonData;
  return (jd as AppJsonData | undefined) ?? undefined;
}
