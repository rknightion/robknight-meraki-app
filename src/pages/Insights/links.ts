import { PLUGIN_BASE_URL, ROUTES } from '../../constants';

/**
 * URL helpers for the Insights area. Each sub-tab gets its own thin wrapper
 * so callers (org cross-links, nav helpers) have a single place to update
 * when/if the route layout changes.
 *
 * Matching the `alertsUrl(orgId)` shape, every helper accepts an optional
 * `orgId` that pre-selects the `$org` variable via `?var-org=` — keeps the
 * cross-link feel consistent with the other scene areas.
 */
export function insightsUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Insights}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

export function licensingUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Insights}/licensing`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

export function apiUsageUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Insights}/api-usage`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

export function clientsUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Insights}/clients`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}
