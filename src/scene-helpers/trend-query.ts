import { SceneQueryRunner } from '@grafana/scenes';
import type { DataSourceRef } from '@grafana/schema';

import type { AppJsonData, RecordingsConfig } from '../types';
import { MERAKI_DS_REF } from './datasource';

/**
 * Options for `trendQueryRunner(...)`. The two branches must describe
 * equivalent shapes so a panel renders identically whether the feature
 * is on or off.
 *
 * - `group` / `template` identify which recording rule feeds this
 *   panel; the helper consults `jsonData.recordings.groups[group]
 *   .rulesEnabled[template]` to decide whether the operator has the rule
 *   turned on. If the rule is disabled (or the operator has not picked a
 *   target datasource at all) the helper returns the *fallback* query
 *   against the nested Meraki DS instead — users always see data.
 *
 * - `recordedExpr` is the full PromQL the panel runs against the
 *   operator-picked Prometheus DS. It should reference the same metric
 *   name the recording rule emits — use the constants exported from
 *   `recording-metrics.ts` to guarantee the template + query stay in
 *   sync.
 *
 * - `fallbackKind` + `fallbackParams` describe the Meraki query-kind
 *   request the panel would have used before recording rules existed.
 *   These keep the non-recording path working exactly as it does today.
 */
export interface TrendQueryOpts<
  TMerakiParams extends Record<string, unknown> = Record<string, unknown>,
> {
  /** Recording-rule group ID (e.g. 'availability'). */
  group: string;
  /** Recording-rule template ID (e.g. 'device-status-overview'). */
  template: string;
  /** PromQL expression for the recorded query. Include all label filters. */
  recordedExpr: string;
  /** Optional legend format string for the recorded query branch. */
  recordedLegend?: string;
  /** Meraki `kind` field to use when falling back to live queries. */
  fallbackKind: string;
  /** Extra fields on the Meraki query model. Merged into the model verbatim. */
  fallbackParams?: TMerakiParams;
  /** refId on the emitted query. Defaults to `'A'`. */
  refId?: string;
}

/**
 * Returns the operator-picked target datasource UID when the feature is
 * fully configured AND the given (group, template) rule is enabled; null
 * otherwise. Callers decide whether to use the recorded path or the
 * direct Meraki fallback based on a non-null result.
 *
 * `jsonData` must come from the plugin's `AppRootProps.meta.jsonData`
 * (threaded down through scene factories). There is no getter for that
 * at scene-build time outside React — see `usePluginMeta()` for the
 * render-time equivalent.
 */
export function resolveRecordingTarget(
  jsonData: AppJsonData | undefined,
  group: string,
  template: string,
): string | null {
  const rec: RecordingsConfig | undefined = jsonData?.recordings;
  if (!rec?.targetDatasourceUid) {
    return null;
  }
  const gState = rec.groups?.[group];
  if (!gState?.installed) {
    return null;
  }
  if (gState.rulesEnabled?.[template] !== true) {
    return null;
  }
  return rec.targetDatasourceUid;
}

/**
 * Builds a `SceneQueryRunner` that dispatches to the recorded
 * Prometheus series when the operator has enabled the feature + picked a
 * target datasource + toggled the specific (group, template) rule on;
 * otherwise falls back to the existing nested-DS Meraki query.
 *
 * The branch is computed once at scene-build time — the helper does NOT
 * subscribe to jsonData changes. Operators who flip the feature must
 * reload, which the config-save path already does via
 * `window.location.reload()` (see `CLAUDE.md` gotcha G.15).
 */
export function trendQueryRunner<
  TMerakiParams extends Record<string, unknown> = Record<string, unknown>,
>(
  jsonData: AppJsonData | undefined,
  opts: TrendQueryOpts<TMerakiParams>,
): SceneQueryRunner {
  const refId = opts.refId ?? 'A';
  const targetUid = resolveRecordingTarget(jsonData, opts.group, opts.template);

  if (targetUid) {
    const promDs: DataSourceRef = { uid: targetUid, type: 'prometheus' };
    return new SceneQueryRunner({
      datasource: promDs,
      queries: [
        {
          refId,
          expr: opts.recordedExpr,
          legendFormat: opts.recordedLegend,
        },
      ],
    });
  }

  return new SceneQueryRunner({
    datasource: MERAKI_DS_REF,
    queries: [
      {
        refId,
        queryKind: opts.fallbackKind,
        ...(opts.fallbackParams ?? {}),
      },
    ],
  });
}
