import { PLUGIN_BASE_URL, ROUTES } from '../../constants';

/**
 * URL for the top-level Audit Log scene. Mirrors the shape of the other
 * per-area `urlFor…` helpers so cross-links don't hand-build paths.
 */
export function urlForAuditLog(): string {
  return `${PLUGIN_BASE_URL}/${ROUTES.AuditLog}`;
}
