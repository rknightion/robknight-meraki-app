import { SceneAppPage } from '@grafana/scenes';
import { PLUGIN_BASE_URL, ROUTES } from '../../constants';
import { auditLogScene } from './auditLogScene';

/**
 * Top-level Audit Log page. Single-scene page (no drilldowns yet) — the
 * `routePath` uses a `/*` suffix so future per-admin or per-network
 * drilldowns slot in without reworking the parent.
 */
export const auditLogPage = new SceneAppPage({
  title: 'Audit Log',
  subTitle: 'Configuration change log — who changed what, when.',
  titleIcon: 'history',
  url: `${PLUGIN_BASE_URL}/${ROUTES.AuditLog}`,
  routePath: `${ROUTES.AuditLog}/*`,
  getScene: () => auditLogScene(),
});
