import { PLUGIN_BASE_URL, ROUTES } from '../../constants';

/**
 * URL for the top-level Switches scene. Optional orgId cross-link pre-selects
 * the org variable, matching the `sensorsUrl(orgId)` shape so navigation feels
 * consistent across scene areas.
 */
export function switchesUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Switches}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

/**
 * URL for the per-switch detail page. Defaults to the bare base so Scenes'
 * default-tab behavior picks Overview; pass `/overview` or `/ports` explicitly
 * if a caller needs to deep-link to a tab.
 */
export function switchDetailUrl(serial: string, orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Switches}/${encodeURIComponent(serial)}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

/**
 * URL for the per-port detail page nested under a switch's Ports tab. Port
 * detail pages live at `/switches/:serial/ports/:portId` so the Ports tab
 * drilldown wildcard (`ports/:portId/*`) can resolve them.
 */
export function portDetailUrl(serial: string, portId: string, orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Switches}/${encodeURIComponent(
    serial
  )}/ports/${encodeURIComponent(portId)}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}
