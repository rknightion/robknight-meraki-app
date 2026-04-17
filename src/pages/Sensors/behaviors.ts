import {
  SceneComponentProps,
  SceneObject,
  SceneObjectBase,
  SceneObjectState,
  sceneGraph,
} from '@grafana/scenes';
import React from 'react';
import { LoadingState } from '@grafana/data';

/**
 * Behavior that sets `isHidden: true` on its parent SceneObject when the
 * nearest data provider returns an empty series list. Useful on the sensor
 * detail page where we stack every metric panel — most sensors only report
 * a subset, so hiding empty panels keeps the page tidy without a per-metric
 * capability check.
 *
 * Implementation notes:
 *  - We wait for `Loading` to resolve before hiding; otherwise the panel
 *    would flicker on every panel mount.
 *  - We inspect `series.length === 0` (no frames) AND `series.every(frame.length === 0)`
 *    (all frames empty) to cover both the "error placeholder frame" case
 *    and the "empty result" case.
 *  - `isHidden` is honoured by SceneFlexLayout — when true, the parent item
 *    collapses and does not occupy space.
 */
interface HideWhenEmptyState extends SceneObjectState {}

export class HideWhenEmpty extends SceneObjectBase<HideWhenEmptyState> {
  public static Component = ({}: SceneComponentProps<HideWhenEmpty>) => {
    return React.createElement(React.Fragment);
  };

  public constructor() {
    super({});
    this.addActivationHandler(() => {
      const dataProvider = sceneGraph.getData(this);
      if (!dataProvider) {
        return;
      }
      const sub = dataProvider.subscribeToState((state) => {
        // Don't flicker while the panel is still fetching — only hide after
        // the query has completed at least once.
        if (state.data?.state !== LoadingState.Done && state.data?.state !== LoadingState.Error) {
          return;
        }
        const series = state.data?.series ?? [];
        const nonEmpty = series.some((frame) => frame.length > 0);
        const parent = getVisualParent(this);
        if (!parent) {
          return;
        }
        parent.setState({ isHidden: !nonEmpty });
      });
      return () => sub.unsubscribe();
    });
  }
}

/** Scene objects whose state carries an `isHidden` flag. */
type Hidable = SceneObject<SceneObjectState & { isHidden?: boolean }>;

/**
 * Walk up from a behavior to the nearest ancestor that supports `isHidden`.
 * Behaviors themselves have no visibility knob; in practice we want to hide
 * the FlexItem that wraps the panel, which is this object's grandparent
 * (behavior → parent SceneObject → FlexItem).
 */
function getVisualParent(obj: SceneObject): Hidable | undefined {
  let cur: SceneObject | undefined = obj.parent;
  while (cur) {
    const state = cur.state as SceneObjectState & { isHidden?: boolean };
    if ('isHidden' in state) {
      return cur as Hidable;
    }
    cur = cur.parent;
  }
  return undefined;
}
