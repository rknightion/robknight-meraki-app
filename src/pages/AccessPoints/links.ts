import { PLUGIN_BASE_URL, ROUTES } from '../../constants';

/**
 * URL for the top-level Access Points scene. Optional orgId cross-link
 * pre-selects the org variable, matching the `sensorsUrl(orgId)` shape so
 * the navigation feel is consistent across scene areas.
 */
export function accessPointsUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.AccessPoints}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

/**
 * URL for the per-AP detail page. Defaults to the bare base so Scenes'
 * default-tab behavior picks Overview; pass `/overview`, `/clients`, or
 * `/rf` explicitly if a caller needs to deep-link to a tab.
 */
export function accessPointDetailUrl(serial: string, orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.AccessPoints}/${encodeURIComponent(serial)}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}
