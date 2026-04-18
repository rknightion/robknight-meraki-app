import { PLUGIN_BASE_URL, ROUTES } from '../../constants';

/**
 * URL for the top-level Alerts scene. Optional `orgId` pre-selects the org
 * variable via `?var-org=`, matching the `sensorsUrl(orgId)` shape so the
 * cross-link feel is consistent across scene areas.
 */
export function alertsUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Alerts}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

/**
 * URL for a single alert. Per-alert detail pages don't exist yet — today
 * this just returns the alerts list URL. Kept as a separate helper so
 * callers (e.g. an "Open alert" override link on the table) can be wired
 * up now and light up automatically when the detail route lands.
 *
 * The `id` parameter is intentionally accepted even though it's unused;
 * swapping the body to `${base}/${encodeURIComponent(id)}` is the single
 * change needed when detail pages ship.
 */
export function alertsDetailUrl(_id: string): string {
  return alertsUrl();
}
