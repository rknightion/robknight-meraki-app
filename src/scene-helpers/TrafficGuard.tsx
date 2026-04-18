import React, { useEffect, useState } from 'react';
import { lastValueFrom } from 'rxjs';
import {
  SceneComponentProps,
  SceneFlexItem,
  SceneObjectBase,
  SceneObjectState,
  SceneReactObject,
  VariableDependencyConfig,
  sceneGraph,
} from '@grafana/scenes';
import { getBackendSrv } from '@grafana/runtime';
import { Alert } from '@grafana/ui';
import { PLUGIN_ID } from '../constants';
import { QueryKind } from '../datasource/types';

/**
 * MetricFindResponse mirrors the wire shape of the app plugin's
 * /resources/metricFind endpoint — we deliberately avoid pulling in the
 * datasource module here so the helper stays cheap to import in any scene.
 */
type MetricFindResponse = {
  values?: Array<{ text?: string; value?: string | number }>;
};

/** QueryResponse mirrors /resources/query — frames are JSON-encoded data.Frame. */
type QueryResponse = {
  frames?: string[] | object[];
};

/**
 * State for {@link TrafficGuardSceneObject}. `networksTemplate` is the raw
 * template string (e.g. `${network}`) which Scenes interpolates against the
 * current `$network` selection at render time.
 */
interface TrafficGuardState extends SceneObjectState {
  networksTemplate: string;
}

/**
 * Decoded shape of one row returned by the `networkTrafficAnalysisMode`
 * handler. The handler emits a wide table frame; this is the per-row tuple
 * after we walk the frame fields.
 */
interface NetworkAnalysisRow {
  networkId: string;
  mode: string;
}

/**
 * TrafficGuardSceneObject mirrors the `DynamicTitle` pattern from the
 * @grafana/scenes "Variables in custom scene objects" guide:
 * `_variableDependency` declares the state path that holds the template, and
 * the React component calls `sceneGraph.interpolate` to resolve `${network}`
 * to the current variable value. Without the dependency declaration, scene
 * variable changes wouldn't trigger a re-render.
 */
class TrafficGuardSceneObject extends SceneObjectBase<TrafficGuardState> {
  static Component = TrafficGuardRenderer;

  protected _variableDependency = new VariableDependencyConfig(this, {
    statePaths: ['networksTemplate'],
  });
}

function TrafficGuardRenderer({ model }: SceneComponentProps<TrafficGuardSceneObject>) {
  const { networksTemplate } = model.useState();
  const interpolated = sceneGraph.interpolate(model, networksTemplate);
  return <TrafficGuard selectedNetworks={interpolated} />;
}

/**
 * Parses the `$network` interpolation result into a clean network-id list.
 *
 * Grafana renders multi-value variables with the configured `allValue`
 * (we use `''`) when the user selects "All", which yields an empty string
 * here. In that case we hydrate the full list via metricFindQuery so the
 * banner audit covers every network in the org rather than silently
 * skipping the check.
 *
 * Single / multi values come through as a comma-separated string per the
 * `csv` formatter in `applyTemplateVariables` — so we split on comma and
 * filter empties.
 */
function parseSelectedNetworks(raw: string): string[] {
  if (!raw) {
    return [];
  }
  return raw
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
}

/**
 * Renders a "traffic analysis disabled for this network" banner above the
 * Page C scene whenever any of the user-selected networks has its
 * `mode` set to `disabled`. The check is best-effort: any HTTP / decode
 * failure is swallowed (returning `null`) so a flaky network call never
 * blocks the page itself.
 *
 * The component intentionally does NOT block rendering of the underlying
 * panels — empty L7 charts on a disabled network are still useful because
 * the banner explains the cause and points operators at the remediation
 * (enable analysis on Network-wide → General).
 */
export function TrafficGuard({ selectedNetworks }: { selectedNetworks: string }) {
  const [disabledNetworks, setDisabledNetworks] = useState<string[]>([]);

  useEffect(() => {
    let cancelled = false;

    (async () => {
      try {
        const networkIds = parseSelectedNetworks(selectedNetworks);
        const effectiveIds =
          networkIds.length > 0 ? networkIds : await listNetworkIdsForAllSelector();

        if (effectiveIds.length === 0) {
          if (!cancelled) {
            setDisabledNetworks([]);
          }
          return;
        }

        const obs = getBackendSrv().fetch<QueryResponse>({
          url: `/api/plugins/${PLUGIN_ID}/resources/query`,
          method: 'POST',
          showErrorAlert: false,
          data: {
            queries: [
              {
                refId: 'TG',
                kind: QueryKind.NetworkTrafficAnalysisMode,
                networkIds: effectiveIds,
              },
            ],
          },
        });
        const { data } = await lastValueFrom(obs);
        if (cancelled) {
          return;
        }
        const rows = decodeAnalysisModeFrames(data?.frames);
        const disabled = rows
          .filter((r) => r.mode === 'disabled')
          .map((r) => r.networkId);
        setDisabledNetworks(disabled);
      } catch {
        // Swallow the failure: the banner is a hint, not load-bearing.
        if (!cancelled) {
          setDisabledNetworks([]);
        }
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [selectedNetworks]);

  if (disabledNetworks.length === 0) {
    return null;
  }

  const networkLabel = disabledNetworks.length === 1
    ? `network ${disabledNetworks[0]}`
    : `${disabledNetworks.length} networks`;

  return (
    <Alert severity="info" title={`Traffic analysis disabled for ${networkLabel}`}>
      <p>
        Meraki only emits L7 application breakdown data when traffic analysis is
        enabled for a network. Enable it on Network-wide → General → Traffic
        analysis to populate the panels below for this network.
      </p>
      {disabledNetworks.length > 1 && (
        <p>
          Affected networks: {disabledNetworks.join(', ')}
        </p>
      )}
    </Alert>
  );
}

/**
 * Hydrates the list of network IDs in the current org when the user has
 * picked the "All" sentinel on the `$network` variable. We can't issue the
 * mode lookup with no network ids — Meraki's settings endpoint is
 * per-network — so we fall back to the same metricFind call the variable
 * uses internally. Returns an empty array on failure (the guard then
 * silently degrades to "no banner").
 */
async function listNetworkIdsForAllSelector(): Promise<string[]> {
  try {
    const obs = getBackendSrv().fetch<MetricFindResponse>({
      url: `/api/plugins/${PLUGIN_ID}/resources/metricFind`,
      method: 'POST',
      showErrorAlert: false,
      data: {
        query: {
          refId: 'TG-all',
          kind: QueryKind.Networks,
          orgId: '$org',
        },
      },
    });
    const { data } = await lastValueFrom(obs);
    if (!data?.values) {
      return [];
    }
    return data.values
      .map((v) => (typeof v.value === 'string' ? v.value : ''))
      .filter((s) => s.length > 0);
  } catch {
    return [];
  }
}

/**
 * Walks the wire-format frames returned by /resources/query and pulls out
 * the `(networkId, mode)` tuples the analysis-mode handler emits. The frames
 * are JSON-encoded data.Frame objects that may arrive either as raw strings
 * (older path) or as already-decoded objects depending on the Grafana
 * runtime version — we tolerate both.
 */
function decodeAnalysisModeFrames(frames: string[] | object[] | undefined): NetworkAnalysisRow[] {
  if (!frames || frames.length === 0) {
    return [];
  }
  const out: NetworkAnalysisRow[] = [];
  for (const raw of frames) {
    let frame: any;
    try {
      frame = typeof raw === 'string' ? JSON.parse(raw) : raw;
    } catch {
      continue;
    }
    const fields = frame?.schema?.fields as Array<{ name: string }> | undefined;
    const values = frame?.data?.values as unknown[][] | undefined;
    if (!fields || !values) {
      continue;
    }
    const networkIdx = fields.findIndex((f) => f.name === 'networkId');
    const modeIdx = fields.findIndex((f) => f.name === 'mode');
    if (networkIdx < 0 || modeIdx < 0) {
      continue;
    }
    const networkCol = (values[networkIdx] ?? []) as string[];
    const modeCol = (values[modeIdx] ?? []) as string[];
    const len = Math.min(networkCol.length, modeCol.length);
    for (let i = 0; i < len; i++) {
      out.push({ networkId: String(networkCol[i] ?? ''), mode: String(modeCol[i] ?? '') });
    }
  }
  return out;
}

/**
 * Wrap {@link TrafficGuard} as a SceneFlexItem. Mirrors `configGuardFlexItem`
 * so scenes can drop the banner in as their first child.
 *
 * Network ids are sourced from the parent scene's `$network` variable via the
 * `${network}` template string. {@link TrafficGuardSceneObject} declares the
 * variable dependency so a variable change re-runs the lookup.
 *
 * The body is a small SceneReactObject whose component renders the
 * scene-object's React tree — this preserves the variable interpolation
 * pipeline while still slotting into a `SceneFlexLayout` like the other
 * helpers.
 */
export function trafficGuardFlexItem(): SceneFlexItem {
  const guard = new TrafficGuardSceneObject({
    networksTemplate: '${network}',
  });
  return new SceneFlexItem({
    body: new SceneReactObject({
      component: () => <guard.Component model={guard} />,
    }),
  });
}
