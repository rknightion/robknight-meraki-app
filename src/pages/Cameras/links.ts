import { PLUGIN_BASE_URL, ROUTES } from '../../constants';

/**
 * URL for the top-level Cameras scene. Optional `orgId` cross-link pre-selects
 * the org variable via `?var-org=`, matching the convention established by
 * {@link accessPointsUrl} / {@link switchesUrl} so cross-family navigation
 * feels consistent.
 */
export function camerasUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Cameras}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

/**
 * URL for the per-camera detail page. Defaults to the bare base so Scenes'
 * default-tab behaviour picks Overview; pass `/overview`, `/analytics`, or
 * `/zones` explicitly to deep-link to a specific tab.
 */
export function cameraDetailUrl(serial: string, orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Cameras}/${encodeURIComponent(serial)}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}
