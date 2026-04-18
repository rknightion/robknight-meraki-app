import { PLUGIN_BASE_URL, ROUTES } from '../../constants';

/**
 * URL for the top-level Events scene. Optional `orgId` pre-selects the org
 * variable via `?var-org=`, matching the convention used by the per-family
 * URL helpers.
 */
export function eventsUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Events}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

/**
 * URL for the Events scene pre-filtered to one network. The Meraki events
 * feed is per-network at the API level so this is the most common cross-
 * link shape (e.g. an alert's network-column cross-link).
 */
export function eventsForNetworkUrl(networkId: string): string {
  return `${PLUGIN_BASE_URL}/${ROUTES.Events}?var-network=${encodeURIComponent(networkId)}`;
}

/**
 * URL for the Events scene pre-filtered to one device. Also pins the
 * product-type selector because the backend requires it. The device serial
 * rides through `var-mg` / `var-ap` / etc. is NOT used here — instead the
 * scene's backend wiring expects serials on the query (frontend work to
 * plumb that as a free variable is left for a follow-up).
 */
export function eventsForDeviceUrl(serial: string, productType: string): string {
  return `${PLUGIN_BASE_URL}/${ROUTES.Events}?var-productType=${encodeURIComponent(
    productType
  )}&var-device=${encodeURIComponent(serial)}`;
}
