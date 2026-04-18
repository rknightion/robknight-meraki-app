import { PLUGIN_BASE_URL, ROUTES } from '../constants';
import type { MerakiProductType } from '../types';

export function organizationsUrl(): string {
  return `${PLUGIN_BASE_URL}/${ROUTES.Organizations}`;
}

export function organizationDetailUrl(orgId: string): string {
  return `${PLUGIN_BASE_URL}/${ROUTES.Organizations}/${encodeURIComponent(orgId)}`;
}

export function sensorsUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Sensors}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

export function sensorDetailUrl(serial: string, orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Sensors}/${encodeURIComponent(serial)}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

export function accessPointsUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.AccessPoints}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

export function accessPointDetailUrl(serial: string, orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.AccessPoints}/${encodeURIComponent(serial)}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

export function switchesUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Switches}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

export function switchDetailUrl(serial: string, orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Switches}/${encodeURIComponent(serial)}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

export function appliancesUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Appliances}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

export function applianceDetailUrl(serial: string, orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Appliances}/${encodeURIComponent(serial)}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

export function camerasUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Cameras}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

export function cameraDetailUrl(serial: string, orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Cameras}/${encodeURIComponent(serial)}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

export function cellularGatewaysUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.CellularGateways}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

export function cellularGatewayDetailUrl(serial: string, orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.CellularGateways}/${encodeURIComponent(serial)}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

export function insightsUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Insights}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

export function eventsUrl(orgId?: string): string {
  const base = `${PLUGIN_BASE_URL}/${ROUTES.Events}`;
  return orgId ? `${base}?var-org=${encodeURIComponent(orgId)}` : base;
}

/**
 * Map a Meraki productType string to the right per-family detail page URL.
 * Used wherever a frame carries heterogeneous device rows (org devices table,
 * alerts list, network events) and the drilldown must route based on the row's
 * productType rather than a hardcoded page.
 *
 * Unknown product types fall back to the sensor detail URL — safe because MT
 * is the original device family this plugin supported and its detail page
 * degrades gracefully for non-sensor serials (empty "latest readings" panel).
 */
export function deviceDrilldownUrl(
  productType: string,
  serial: string,
  orgId?: string
): string {
  switch (productType as MerakiProductType) {
    case 'wireless':
      return accessPointDetailUrl(serial, orgId);
    case 'switch':
      return switchDetailUrl(serial, orgId);
    case 'appliance':
      return applianceDetailUrl(serial, orgId);
    case 'camera':
      return cameraDetailUrl(serial, orgId);
    case 'cellularGateway':
      return cellularGatewayDetailUrl(serial, orgId);
    case 'sensor':
    default:
      return sensorDetailUrl(serial, orgId);
  }
}
