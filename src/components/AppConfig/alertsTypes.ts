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
 *
 * Generic bundle shapes (shared with recording rules) live under
 * `./RuleBundlePanel/types.ts`. The interfaces here extend those bases with
 * alert-specific fields (e.g. `severity`).
 */

import { AlertsReconcileSummary } from '../../types';
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

export interface AlertTemplateDef extends RuleTemplateDef {
  severity: string;
}

export type AlertGroupDef = RuleGroupDef<AlertTemplateDef>;

export interface AlertsTemplatesResponse {
  groups: AlertGroupDef[];
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

export type AlertsGroupStateDto = GroupStateDto;

export type DesiredStatePayload = BundleDesiredState;
