/**
 * Wire-format types for the `/alerts/*` resource endpoints. These mirror the
 * Go DTOs in `pkg/plugin/resources.go` (alertThresholdSchemaDTO,
 * alertTemplateDTO, alertGroupDTO, alertsTemplatesResponse,
 * alertsInstalledRuleDTO, alertsStatusResponse, desiredStateDTO) — keep both
 * sides in sync when adding or renaming fields.
 *
 * The frontend's `AlertsConfig` / `AlertsGroupState` in `src/types.ts` are
 * the *persisted* view (what ends up in plugin jsonData). The types here are
 * the *transport* view exchanged with the resource endpoints. They overlap
 * heavily but differ around ephemeral fields (lastReconcileSummary, etc.).
 */

import { AlertsReconcileSummary } from '../../types';

/**
 * Threshold metadata used to render an editor control. `type` drives the
 * input widget in AlertRulesPanel; `default` is whatever the Go registry
 * encoded (string / number / boolean / string[]) and is passed through
 * `json.RawMessage` on the wire — the TS side treats it as `unknown` and
 * coerces when rendering.
 */
export interface ThresholdSchemaDef {
  key: string;
  type: 'int' | 'float' | 'string' | 'duration' | 'list' | string;
  default?: unknown;
  label?: string;
  help?: string;
  options?: string[];
}

export interface AlertTemplateDef {
  id: string;
  groupId: string;
  displayName: string;
  severity: string;
  thresholds: ThresholdSchemaDef[];
}

export interface AlertGroupDef {
  id: string;
  displayName: string;
  templates: AlertTemplateDef[];
}

export interface AlertsTemplatesResponse {
  groups: AlertGroupDef[];
}

/**
 * Info on a single currently-installed rule. Mirrors
 * `alertsInstalledRuleDTO`. Enabled=false means the rule exists in Grafana
 * but is paused — the reconciler treats this as a legitimate end state when
 * the user has flipped the checkbox off.
 */
export interface InstalledRuleInfo {
  groupId: string;
  templateId: string;
  orgId: string;
  uid: string;
  enabled: boolean;
}

export interface AlertsStatusResponse {
  installed: InstalledRuleInfo[];
  lastReconciledAt?: string;
  lastReconcileSummary?: AlertsReconcileSummary;
  /**
   * Whether the Grafana provisioning API probe succeeded. When false, the
   * UI renders a feature-toggle banner prompting the operator to enable
   * externalServiceAccounts.
   */
  grafanaReady: boolean;
}

/** Single-rule failure row from ReconcileResult.Failed. */
export interface ReconcileFailureDef {
  uid: string;
  action: 'create' | 'update' | 'delete' | string;
  err: string;
}

/**
 * Synchronous reconcile outcome. Arrays hold UIDs so the UI can show a
 * verbatim "created: X, updated: Y" banner. `startedAt` / `finishedAt` are
 * RFC3339 timestamps encoded by Go's default time.Time JSON marshaller.
 */
export interface ReconcileResultResponse {
  created: string[];
  updated: string[];
  deleted: string[];
  failed: ReconcileFailureDef[];
  startedAt: string;
  finishedAt: string;
}

export interface AlertsGroupStateDto {
  installed: boolean;
  rulesEnabled: Record<string, boolean>;
}

/**
 * Wire payload for POST /alerts/reconcile. Mirrors `desiredStateDTO`. The
 * innermost threshold value type is `unknown` — the backend revalidates
 * against the template schema so the TS layer stays permissive.
 */
export interface DesiredStatePayload {
  groups: Record<string, AlertsGroupStateDto>;
  thresholds?: Record<string, Record<string, Record<string, unknown>>>;
  /**
   * Optional override of the org list the reconciler fans out to. When
   * omitted the Go side calls meraki.Client.ListOrganizations. Primarily a
   * testing/escape hatch; the regular config flow leaves it unset.
   */
  orgOverride?: string[];
}
