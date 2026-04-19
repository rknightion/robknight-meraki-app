/**
 * Shared wire + UI types for bundle-style rule panels (alerts + recording
 * rules). These are the generic shapes every bundle reconcile flow needs —
 * per-template thresholds, per-group install state, and the synchronous
 * reconcile response envelope.
 *
 * Alert-specific fields (e.g. `severity`) live on the more specific types
 * in `../alertsTypes.ts`, which extend the bases defined here. Recording
 * rules will have their own extension type with a canonical metric name
 * instead of severity.
 */

/**
 * Threshold metadata used to render an editor control. `type` drives the
 * input widget in ThresholdInput; `default` is whatever the Go registry
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

export interface RuleTemplateDef {
  id: string;
  groupId: string;
  displayName: string;
  thresholds: ThresholdSchemaDef[];
}

export interface RuleGroupDef<T extends RuleTemplateDef = RuleTemplateDef> {
  id: string;
  displayName: string;
  templates: T[];
}

/**
 * Info on a single currently-installed rule. Mirrors the Go-side
 * `installedRuleDTO`. Enabled=false means the rule exists in Grafana but is
 * paused — the reconciler treats this as a legitimate end state when the
 * user has flipped the checkbox off.
 */
export interface InstalledRuleInfo {
  groupId: string;
  templateId: string;
  orgId: string;
  uid: string;
  enabled: boolean;
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

export interface GroupStateDto {
  installed: boolean;
  rulesEnabled: Record<string, boolean>;
}

/**
 * Wire payload for POST /<bundle>/reconcile. The innermost threshold value
 * type is `unknown` — the backend revalidates against the template schema
 * so the TS layer stays permissive.
 */
export interface BundleDesiredState {
  groups: Record<string, GroupStateDto>;
  thresholds?: Record<string, Record<string, Record<string, unknown>>>;
  /**
   * Optional override of the org list the reconciler fans out to. When
   * omitted the Go side calls meraki.Client.ListOrganizations. Primarily a
   * testing/escape hatch; the regular config flow leaves it unset.
   */
  orgOverride?: string[];
}

/**
 * Persisted slice of jsonData shared by every bundle panel. Only the user's
 * toggles + threshold overrides — runtime reconcile telemetry lives on the
 * status endpoint, not here.
 */
export interface BundleSavedConfig {
  groups?: Record<string, GroupStateDto>;
  thresholds?: Record<string, Record<string, Record<string, unknown>>>;
}
