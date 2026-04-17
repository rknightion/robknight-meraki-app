import { PLUGIN_BASE_URL, ROUTES } from '../constants';

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
