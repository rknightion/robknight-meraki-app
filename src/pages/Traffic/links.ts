import { PLUGIN_BASE_URL, ROUTES } from '../../constants';

/**
 * URL for the top-level Traffic Analytics scene. Optional `orgId` /
 * `networkId` pre-select the matching scene variables via the
 * `?var-org=` / `?var-network=` query params — same convention as
 * `alertsUrl(orgId)` so cross-page navigation feels consistent.
 *
 * `networkId` is intentionally singular even though the page allows
 * multi-select on `$network`: callers that want to pre-filter to one
 * network will pass that one id, and the user can broaden it from the
 * scene if needed.
 */
export function trafficUrl(opts?: { orgId?: string; networkId?: string }): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Traffic}`;
  const parts: string[] = [];
  if (opts?.orgId) {
    parts.push(`var-org=${encodeURIComponent(opts.orgId)}`);
  }
  if (opts?.networkId) {
    parts.push(`var-network=${encodeURIComponent(opts.networkId)}`);
  }
  return parts.length > 0 ? `${base}?${parts.join('&')}` : base;
}
