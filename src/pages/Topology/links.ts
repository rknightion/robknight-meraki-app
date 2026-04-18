import { PLUGIN_BASE_URL, ROUTES } from '../../constants';

/**
 * URL for the top-level Topology page. Optional orgId pre-selects the
 * `$org` variable so cross-area links from Home / Organizations land
 * with the right context already wired.
 */
export function topologyUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Topology}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}
