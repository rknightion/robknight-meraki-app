import { PLUGIN_BASE_URL, ROUTES } from '../../constants';

/**
 * URL for the top-level Clients page. Mirrors the shape of the other per-area
 * `urlFor…` helpers so cross-links don't hand-build paths.
 */
export function urlForClients(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Clients}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

/**
 * Per-client drilldown URL. `mac` is encoded so a colon-separated MAC
 * survives URL routing. Forwards an optional `org` so the child scene can
 * hydrate `$org` without re-asking the variable picker.
 */
export function urlForClient(mac: string, orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Clients}/${encodeURIComponent(mac)}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}
