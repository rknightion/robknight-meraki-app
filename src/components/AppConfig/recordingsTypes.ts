/**
 * Wire-format types for the `/recordings/*` resource endpoints. Mirrors
 * the Go DTOs in `pkg/plugin/resources.go`
 * (recordingThresholdSchemaDTO → alertThresholdSchemaDTO reused;
 * recordingTemplateDTO, recordingGroupDTO, recordingsTemplatesResponse,
 * recordingsInstalledRuleDTO, recordingsStatusResponse,
 * recordingsDesiredStateDTO) — keep both sides in sync when adding or
 * renaming fields.
 *
 * The frontend's `RecordingsConfig` / `RecordingsGroupState` in
 * `src/types.ts` are the *persisted* view (what ends up in plugin
 * jsonData). The types here are the *transport* view exchanged with the
 * resource endpoints. They overlap but differ around the
 * target-datasource-uid gate and the runtime last-reconcile telemetry.
 *
 * Generic bundle shapes (shared with alert rules) live under
 * `./RuleBundlePanel/types.ts`. The interfaces here extend those bases
 * with recording-specific fields (e.g. `metric`, `targetDatasourceUid`).
 */

import { RecordingsReconcileSummary } from '../../types';
import {
  BundleDesiredState,
  GroupStateDto,
  InstalledRuleInfo,
  ReconcileFailureDef,
  ReconcileResultResponse,
  RuleGroupDef,
  RuleTemplateDef,
  ThresholdSchemaDef,
} from './RuleBundlePanel/types';

export type {
  InstalledRuleInfo,
  ReconcileFailureDef,
  ReconcileResultResponse,
  ThresholdSchemaDef,
};

/**
 * A single recording-rule template. Extends the generic RuleTemplateDef
 * with the Prometheus metric name the rule emits — surfaced in the UI so
 * operators can cross-reference template → series when debugging a
 * dashboard.
 */
export interface RecordingTemplateDef extends RuleTemplateDef {
  metric: string;
}

export type RecordingGroupDef = RuleGroupDef<RecordingTemplateDef>;

export interface RecordingsTemplatesResponse {
  groups: RecordingGroupDef[];
}

export interface RecordingsStatusResponse {
  installed: InstalledRuleInfo[];
  /**
   * Echo of jsonData.recordings.targetDatasourceUid so the panel can
   * render the current selection without re-reading plugin settings.
   * Empty means the operator has not yet picked a target.
   */
  targetDatasourceUid?: string;
  lastReconciledAt?: string;
  lastReconcileSummary?: RecordingsReconcileSummary;
  /**
   * Whether the Grafana provisioning API probe succeeded. Same
   * `externalServiceAccounts` feature-toggle gate as alerts.
   */
  grafanaReady: boolean;
}

export type RecordingsGroupStateDto = GroupStateDto;

/**
 * Reconcile payload. Extends BundleDesiredState with an optional
 * targetDatasourceUid — the backend authoritatively reads the UID from
 * jsonData and this field is mainly a hermetic-testing escape hatch.
 */
export interface RecordingsDesiredStatePayload extends BundleDesiredState {
  targetDatasourceUid?: string;
}
