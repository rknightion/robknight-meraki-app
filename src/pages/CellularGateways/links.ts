import { PLUGIN_BASE_URL, ROUTES } from '../../constants';

/**
 * URL for the top-level Cellular Gateways scene. Optional `orgId` cross-link
 * pre-selects the org variable via `?var-org=`, matching the convention used
 * by {@link accessPointsUrl} / {@link switchesUrl} / {@link camerasUrl}.
 */
export function cellularGatewaysUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.CellularGateways}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

/**
 * URL for the per-gateway detail page. Defaults to the bare base so Scenes'
 * default-tab behaviour picks Overview; pass `/overview`, `/uplink`, or
 * `/port-forwarding` explicitly to deep-link to a specific tab.
 */
export function cellularGatewayDetailUrl(serial: string, orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.CellularGateways}/${encodeURIComponent(serial)}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}
