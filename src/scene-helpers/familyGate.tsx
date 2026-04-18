import React from 'react';
import {
  EmbeddedScene,
  SceneComponentProps,
  SceneObject,
  SceneObjectBase,
  SceneObjectState,
  sceneGraph,
} from '@grafana/scenes';
import { Alert, LinkButton } from '@grafana/ui';
import { usePluginMeta } from '../utils/utils.plugin';
import { ROUTES } from '../constants';
import { prefixRoute } from '../utils/utils.routing';
import { useOrgDeviceFamilies } from './orgDeviceFamilies';
import type { AppJsonData, MerakiProductType } from '../types';

/**
 * FamilyGatedLayout wraps a scene body and conditionally replaces it with a
 * banner when the selected org has zero devices of `family`. Mounted as the
 * body of an existing EmbeddedScene so the scene's variables, time range, and
 * controls (which SceneAppPage renders in its header) stay attached.
 *
 * The original body is held in state — never unmounted from the scene graph —
 * so toggling the "Show empty device-family pages" setting on/off preserves
 * in-flight query state inside the wrapped body.
 */
interface FamilyGatedLayoutState extends SceneObjectState {
  family: MerakiProductType;
  wrapped: SceneObject;
}

export class FamilyGatedLayout extends SceneObjectBase<FamilyGatedLayoutState> {
  static Component = FamilyGatedLayoutRenderer;
}

function FamilyGatedLayoutRenderer({ model }: SceneComponentProps<FamilyGatedLayout>) {
  const { family, wrapped } = model.useState();

  // Pull the current $org value from the scene graph. The variable lives on
  // the enclosing EmbeddedScene (injected by every device-family overview
  // scene via `orgVariable()`).
  const orgVar = sceneGraph.lookupVariable('org', model);
  const rawValue = orgVar?.getValue?.();
  const orgId = Array.isArray(rawValue) ? String(rawValue[0] ?? '') : String(rawValue ?? '');

  const { families, loading } = useOrgDeviceFamilies(orgId || null);
  const meta = usePluginMeta();
  const jsonData = meta?.jsonData as AppJsonData | undefined;
  const showEmpty = Boolean(jsonData?.showEmptyFamilies);

  const familyCount = families[family] ?? 0;
  const shouldGate = Boolean(orgId) && !loading && !showEmpty && familyCount === 0;

  if (shouldGate) {
    return <FamilyEmptyAlert family={family} />;
  }

  const Comp = wrapped.Component;
  return <Comp model={wrapped} />;
}

const FAMILY_LABELS: Record<MerakiProductType, string> = {
  appliance: 'appliance (MX)',
  wireless: 'access point (MR)',
  switch: 'switch (MS)',
  camera: 'camera (MV)',
  cellularGateway: 'cellular gateway (MG)',
  sensor: 'sensor (MT)',
};

function FamilyEmptyAlert({ family }: { family: MerakiProductType }) {
  const label = FAMILY_LABELS[family] ?? family;
  return (
    <Alert severity="info" title={`No ${label} devices in this organisation`}>
      <p>
        Select a different organisation in the top toolbar, or enable
        &ldquo;Show empty device-family pages&rdquo; in the Meraki Configuration
        page if you want this scene visible anyway.
      </p>
      <LinkButton
        href={prefixRoute(ROUTES.Configuration)}
        variant="primary"
        icon="cog"
      >
        Open configuration
      </LinkButton>
    </Alert>
  );
}

/**
 * Wrap an existing scene factory so the produced scene's body is gated behind
 * the family-device check. Usage at a SceneAppPage:
 *
 *     getScene: familyGateWrap('appliance', () => appliancesScene())
 *
 * Preserves the scene's $variables / $timeRange / controls so SceneAppPage's
 * header still renders the time picker and variable selectors.
 */
export function familyGateWrap(
  family: MerakiProductType,
  buildScene: () => EmbeddedScene
): () => EmbeddedScene {
  return () => {
    const scene = buildScene();
    const original = scene.state.body;
    scene.setState({
      body: new FamilyGatedLayout({ family, wrapped: original }),
    });
    return scene;
  };
}
