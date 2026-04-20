import {
  SceneComponentProps,
  SceneDataProvider,
  SceneObject,
  SceneObjectBase,
  SceneObjectState,
} from '@grafana/scenes';
import React from 'react';
import { DataFrame, FieldType, LoadingState } from '@grafana/data';

/**
 * Behavior that sets `isHidden: true` on its parent SceneObject when the
 * wrapped panel's query runner returns an empty series list. Useful on the
 * sensor detail page and overview metric grid — most sensors only report a
 * subset of metrics, and Grafana's statetimeline/timeseries vizes surface a
 * noisy "Data does not have a time field" error on zero-row frames instead
 * of honouring the panel's `noValue` text.
 *
 * Data-provider lookup:
 *
 *  - Walks *down* from its parent (via `state.$data` on the parent itself,
 *    then `state.body` for `SceneFlexItem` wrappers) to find the first
 *    `SceneQueryRunner`-like provider. An earlier version used
 *    `sceneGraph.getData(this)` which walks *up* the tree; on scenes that
 *    declare `$data: SceneDataLayerSet` at the scene level (annotation
 *    overlays) it resolved to the annotations layer instead of the panel's
 *    own runner, so the hide check fired against annotation frames and
 *    never on the actual panel query.
 *  - Data layer sets are skipped explicitly: their state carries `layers`,
 *    which plain query runners do not.
 *
 * Implementation notes:
 *  - We wait for `Done` or `Error` before hiding to avoid flicker on mount.
 *  - `series.some(frame.length > 0)` covers both "no frames" and "all frames
 *    empty" (the two shapes the backend emits on no-data responses).
 *  - `isHidden` is honoured by `SceneFlexLayout` and `SceneCSSGridLayout` —
 *    the wrapper collapses and does not occupy grid space.
 */
type HideWhenEmptyState = SceneObjectState;

export class HideWhenEmpty extends SceneObjectBase<HideWhenEmptyState> {
  public static Component = ({}: SceneComponentProps<HideWhenEmpty>) => {
    return React.createElement(React.Fragment);
  };

  private readonly explicitProvider?: SceneDataProvider;

  /**
   * @param dataProvider - Optional explicit data provider to watch. When
   *   omitted, the behavior walks DOWN from its parent (body/children) to
   *   find the first non-layer-set data provider. Pass an explicit runner
   *   when the panel is buried several layers deep under FlexLayouts.
   */
  public constructor(dataProvider?: SceneDataProvider) {
    super({});
    this.explicitProvider = dataProvider;
    this.addActivationHandler(() => {
      const provider = this.explicitProvider ?? findNonLayerDataProvider(this.parent);
      if (!provider) {
        return;
      }
      const sub = provider.subscribeToState((state) => {
        if (state.data?.state !== LoadingState.Done && state.data?.state !== LoadingState.Error) {
          return;
        }
        const series = state.data?.series ?? [];
        const nonEmpty = series.some(frameHasActualData);
        const parent = this.parent as Hidable | undefined;
        if (!parent) {
          return;
        }
        parent.setState({ isHidden: !nonEmpty });
      });
      // Subscribing only catches FUTURE state transitions. If the runner has
      // already reached Done (cache hit, singleflight sibling already loaded)
      // by the time activation fires, we'd miss it. Evaluate the current
      // state once up front so freshly-cached empty results still collapse.
      const current = (provider.state as { data?: { state?: LoadingState; series?: unknown[] } }).data;
      if (current?.state === LoadingState.Done || current?.state === LoadingState.Error) {
        const series = (current.series ?? []) as DataFrame[];
        const nonEmpty = series.some(frameHasActualData);
        const parent = this.parent as Hidable | undefined;
        if (parent) {
          parent.setState({ isHidden: !nonEmpty });
        }
      }
      return () => sub.unsubscribe();
    });
  }
}

/**
 * Scene objects whose state carries an `isHidden` flag. In practice the
 * direct parent of this behavior is always a SceneFlexItem or
 * SceneCSSGridItem — both of whose renderers honour `isHidden`. We don't
 * walk up looking for an ancestor that already has the key in state,
 * because `SceneFlexItem` and friends omit `isHidden` from state until
 * it's first set, and the `in` operator would then walk past them.
 */
type Hidable = SceneObject<SceneObjectState & { isHidden?: boolean }>;

/**
 * Walk DOWN from the given root to find the first query-runner-like data
 * provider. Skips SceneDataLayerSet (annotation layer) because those emit
 * annotation frames rather than panel data and would cause the hide check
 * to fire against the wrong series list.
 *
 * Traversal order mirrors the common scene wrappers: a SceneFlexItem/
 * SceneCSSGridItem carries the panel in `body`; a VizPanel carries its
 * runner in `$data`. We inspect `$data` first on every object we visit so
 * that attaching the behavior directly to a VizPanel still works.
 */
function findNonLayerDataProvider(start?: SceneObject): SceneDataProvider | undefined {
  if (!start) {
    return undefined;
  }
  const state = start.state as SceneObjectState & {
    $data?: SceneObject;
    body?: SceneObject;
  };
  const maybe = state.$data;
  if (maybe && isSubscribableProvider(maybe) && !isLayerSet(maybe)) {
    return maybe as SceneDataProvider;
  }
  if (state.body) {
    return findNonLayerDataProvider(state.body);
  }
  return undefined;
}

function isSubscribableProvider(o: SceneObject): boolean {
  return typeof (o as unknown as { subscribeToState?: unknown }).subscribeToState === 'function';
}

/**
 * SceneDataLayerSet exposes a `layers` array on its state; SceneQueryRunner
 * does not. This is a duck-type probe rather than an `instanceof` check so
 * the behavior keeps working across @grafana/scenes minor versions where
 * class identity can drift.
 */
function isLayerSet(o: SceneObject): boolean {
  return Array.isArray((o.state as unknown as { layers?: unknown }).layers);
}

/**
 * Treat a frame as "actual data" when it has at least one non-null value on
 * any non-time field. The backend's empty fallback emits a one-row frame
 * with a null value column (so Grafana can still identify the time field
 * and render the panel's `noValue` text); this check differentiates that
 * shape from a frame with real readings so empty panels still collapse.
 */
function frameHasActualData(frame: DataFrame): boolean {
  if (frame.length === 0) {
    return false;
  }
  return frame.fields.some((f) => {
    if (f.type === FieldType.time) {
      return false;
    }
    for (let i = 0; i < frame.length; i++) {
      const v = f.values[i];
      if (v === null || v === undefined) {
        continue;
      }
      if (typeof v === 'number' && Number.isNaN(v)) {
        continue;
      }
      return true;
    }
    return false;
  });
}
