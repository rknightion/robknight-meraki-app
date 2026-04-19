import React, { useEffect, useMemo, useState } from 'react';
import { css } from '@emotion/css';
import { lastValueFrom } from 'rxjs';
import { GrafanaTheme2 } from '@grafana/data';
import { getBackendSrv } from '@grafana/runtime';
import { Alert, ConfirmModal, useStyles2 } from '@grafana/ui';
import { PLUGIN_ID } from '../../constants';
import { AppJsonData } from '../../types';
import { testIds } from '../testIds';
import { useAlertsTemplates } from './useAlertsTemplates';
import { useAlertsStatus } from './useAlertsStatus';
import {
  AlertTemplateDef,
  AlertsStatusResponse,
  AlertsTemplatesResponse,
  DesiredStatePayload,
  ReconcileResultResponse,
} from './alertsTypes';
import {
  ButtonRow,
  ButtonRowTestIds,
  DriftBanner,
  GroupRow,
  GroupRowTestIds,
  GroupStateDto,
  detectDrift,
  indexInstalled,
  seedDesired,
} from './RuleBundlePanel';

const ALERTS_RECONCILE_PATH = 'alerts/reconcile';
const ALERTS_UNINSTALL_PATH = 'alerts/uninstall-all';

export { detectDrift, indexInstalled };

export type AlertRulesPanelProps = {
  jsonData?: AppJsonData;
  title?: string;
  subtitle?: string;
  featureToggleTitle?: string;
  featureToggleBody?: React.ReactNode;
  loadErrorTitle?: string;
  reconcileEndpointPath?: string;
  uninstallEndpointPath?: string;
  viewInGrafanaHref?: string;
  viewInGrafanaLabel?: string;
  groupEmptyHint?: string;
  footerLabels?: string;
  footerFolder?: string;
  footerHint?: string;
  uninstallConfirmTitle?: string;
  uninstallConfirmBody?: string;
};

/**
 * Configuration-page section that lets admins install, tune, and remove the
 * plugin's bundled alert rule groups. Non-trivial data flow:
 *
 *  - `/resources/alerts/templates` is the static registry (shape of each
 *    group + threshold schema).
 *  - `/resources/alerts/status` is the live picture: which rules are in
 *    Grafana right now, plus last-reconcile telemetry.
 *  - Local `desired` state is the user's WIP edit. Initialised from
 *    jsonData.alerts (persisted thresholds) + status.installed (group is
 *    considered installed if any rule under it exists).
 *  - "Reconcile selected" POSTs `desired` to `/resources/alerts/reconcile`.
 *  - "Uninstall all" POSTs to `/resources/alerts/uninstall-all`.
 *
 * Drift detection is best-effort: if status.installed[i].enabled differs
 * from desired.groups[gid].rulesEnabled[tid] we render an info banner. The
 * Go side does NOT currently return live thresholds (only UIDs + paused
 * state), so the threshold side of drift is invisible until that endpoint
 * grows richer — acceptable for now because thresholds only drift when an
 * operator hand-edits a rule in the Grafana Alerting UI.
 */
export function AlertRulesPanel({
  jsonData,
  title = 'Bundled alert rules',
  subtitle = 'Rules are managed by the Meraki plugin. Contact points and notification policies are your responsibility — see the Grafana Alerting UI.',
  featureToggleTitle = 'Alerts bundle unavailable',
  featureToggleBody,
  loadErrorTitle = 'Failed to load alert bundle',
  reconcileEndpointPath = ALERTS_RECONCILE_PATH,
  uninstallEndpointPath = ALERTS_UNINSTALL_PATH,
  viewInGrafanaHref = '/alerting/grouped?dataSource=grafana',
  viewInGrafanaLabel = 'View in Grafana Alerting',
  groupEmptyHint = 'Turn on "Install this group" above to pick per-rule toggles and tune thresholds.',
  footerLabels = 'severity, meraki_group, meraki_product, meraki_org, meraki_rule',
  footerFolder = 'Meraki (bundled)',
  footerHint = "Routing: use Grafana's notification-policy matchers on the labels above to send these rules to the right contact points.",
  uninstallConfirmTitle = 'Uninstall all Meraki alert rules?',
  uninstallConfirmBody = 'This will delete every rule this plugin installed. Continue?',
}: AlertRulesPanelProps) {
  const s = useStyles2(getStyles);

  const {
    data: templates,
    loading: templatesLoading,
    error: templatesError,
  } = useAlertsTemplates();
  const {
    data: status,
    loading: statusLoading,
    error: statusError,
    refetch: refetchStatus,
  } = useAlertsStatus();

  const [desired, setDesired] = useState<DesiredStatePayload>({ groups: {}, thresholds: {} });
  const [desiredInitialised, setDesiredInitialised] = useState(false);
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>({});
  const [confirmUninstall, setConfirmUninstall] = useState(false);
  const [actionInFlight, setActionInFlight] = useState(false);
  const [resultBanner, setResultBanner] = useState<
    null | { kind: 'success' | 'error'; title: string; body: string }
  >(null);

  // Initialise desired state once both templates + status have loaded.
  // Subsequent refetches (after reconcile) should NOT clobber the user's
  // in-progress edits, so we only run this on the first combined load.
  //
  // The setState calls live inside an async IIFE + microtask to satisfy
  // the `react-hooks/set-state-in-effect` lint rule, which forbids
  // synchronous setState in an effect body.
  useEffect(() => {
    if (desiredInitialised || !templates || !status) {
      return;
    }
    let cancelled = false;
    (async () => {
      const seed = seedDesired(templates.groups, status.installed, jsonData?.alerts);
      if (cancelled) {
        return;
      }
      setDesired({ groups: seed.groups, thresholds: seed.thresholds });
      setOpenGroups(seed.openGroups);
      setDesiredInitialised(true);
    })();
    return () => {
      cancelled = true;
    };
  }, [templates, status, jsonData, desiredInitialised]);

  const counts = useMemo(() => computeCounts(templates, status, desired), [templates, status, desired]);
  const drift = useMemo(() => detectDrift(status, desired), [status, desired]);

  const grafanaReady = status?.grafanaReady ?? true;

  const onToggleGroupInstall = (groupId: string, next: boolean) => {
    setDesired((prev) => {
      const prevGroup = prev.groups[groupId] ?? { installed: false, rulesEnabled: {} };
      return {
        ...prev,
        groups: {
          ...prev.groups,
          [groupId]: { ...prevGroup, installed: next },
        },
      };
    });
    setOpenGroups((prev) => ({ ...prev, [groupId]: next || prev[groupId] }));
  };

  const onToggleRuleEnabled = (groupId: string, templateId: string, next: boolean) => {
    setDesired((prev) => {
      const prevGroup = prev.groups[groupId] ?? { installed: false, rulesEnabled: {} };
      return {
        ...prev,
        groups: {
          ...prev.groups,
          [groupId]: {
            ...prevGroup,
            rulesEnabled: { ...prevGroup.rulesEnabled, [templateId]: next },
          },
        },
      };
    });
  };

  const onThresholdChange = (
    groupId: string,
    templateId: string,
    key: string,
    value: unknown,
  ) => {
    setDesired((prev) => {
      const nextThresholds = { ...(prev.thresholds ?? {}) };
      const perGroup = { ...(nextThresholds[groupId] ?? {}) };
      const perTpl = { ...(perGroup[templateId] ?? {}) };
      perTpl[key] = value;
      perGroup[templateId] = perTpl;
      nextThresholds[groupId] = perGroup;
      return { ...prev, thresholds: nextThresholds };
    });
  };

  const onReconcile = async () => {
    setActionInFlight(true);
    setResultBanner(null);
    try {
      const result = await postReconcile(reconcileEndpointPath, desired);
      const msg = `Created ${result.created.length} · Updated ${result.updated.length} · Deleted ${result.deleted.length}` +
        (result.failed.length ? ` · Failed ${result.failed.length}` : '');
      setResultBanner({ kind: 'success', title: 'Reconcile complete', body: msg });
      refetchStatus();
    } catch (e) {
      setResultBanner({
        kind: 'error',
        title: 'Reconcile failed',
        body: e instanceof Error ? e.message : String(e),
      });
    } finally {
      setActionInFlight(false);
    }
  };

  const onUninstallAll = async () => {
    setConfirmUninstall(false);
    setActionInFlight(true);
    setResultBanner(null);
    try {
      const result = await postUninstallAll(uninstallEndpointPath);
      setResultBanner({
        kind: 'success',
        title: 'Uninstall complete',
        body: `Deleted ${result.deleted.length} rule(s).`,
      });
      // Clear local desired-state install flags so the UI reflects the
      // freshly-empty world. Thresholds stay in place (user may want to
      // reinstall with the same tuning).
      setDesired((prev) => {
        const next: Record<string, GroupStateDto> = {};
        for (const [gid, gs] of Object.entries(prev.groups)) {
          next[gid] = { ...gs, installed: false };
        }
        return { ...prev, groups: next };
      });
      refetchStatus();
    } catch (e) {
      setResultBanner({
        kind: 'error',
        title: 'Uninstall failed',
        body: e instanceof Error ? e.message : String(e),
      });
    } finally {
      setActionInFlight(false);
    }
  };

  const loading = templatesLoading || statusLoading;
  const loadError = templatesError ?? statusError;

  const groupRowTestIds: GroupRowTestIds = {
    groupCard: testIds.alertRulesPanel.groupCard,
    groupInstallToggle: testIds.alertRulesPanel.groupInstallToggle,
    templateRow: testIds.alertRulesPanel.templateRow,
    ruleEnabled: testIds.alertRulesPanel.ruleEnabled,
    thresholdInput: testIds.alertRulesPanel.thresholdInput,
  };

  const buttonRowTestIds: ButtonRowTestIds = {
    reconcileButton: testIds.alertRulesPanel.reconcileButton,
    uninstallButton: testIds.alertRulesPanel.uninstallButton,
    viewInGrafana: testIds.alertRulesPanel.viewInGrafana,
  };

  return (
    <div className={s.root} data-testid={testIds.alertRulesPanel.container}>
      <h3 className={s.heading}>{title}</h3>
      <p className={s.subtitle}>{subtitle}</p>

      {!grafanaReady && (
        <Alert
          severity="warning"
          title={featureToggleTitle}
          data-testid={testIds.alertRulesPanel.featureToggleBanner}
        >
          {featureToggleBody ?? (
            <>
              Enable the <code>externalServiceAccounts</code> feature toggle in Grafana (or upgrade to a
              build where it is on by default), then reload this page.
            </>
          )}
        </Alert>
      )}

      {loadError && (
        <Alert severity="error" title={loadErrorTitle}>
          {loadError}
        </Alert>
      )}

      {drift && <DriftBanner testId={testIds.alertRulesPanel.driftBanner} />}

      {resultBanner && (
        <Alert
          severity={resultBanner.kind}
          title={resultBanner.title}
          data-testid={testIds.alertRulesPanel.resultBanner}
          onRemove={() => setResultBanner(null)}
        >
          {resultBanner.body}
        </Alert>
      )}

      <div className={s.statusRow} data-testid={testIds.alertRulesPanel.statusPill}>
        <span className={s.pill}>
          {counts.installedGroups} of {counts.totalGroups} groups installed · {counts.installedRules} rules
        </span>
        {status?.lastReconciledAt && (
          <span className={s.pillMuted}>
            Last reconciled {formatRelative(status.lastReconciledAt)}
          </span>
        )}
      </div>

      <ButtonRow
        onReconcile={onReconcile}
        onRequestUninstall={() => setConfirmUninstall(true)}
        viewInGrafanaHref={viewInGrafanaHref}
        viewInGrafanaLabel={viewInGrafanaLabel}
        actionInFlight={actionInFlight}
        loading={loading}
        grafanaReady={grafanaReady}
        installedRules={counts.installedRules}
        testIds={buttonRowTestIds}
      />

      {/*
        Group cards. The install checkbox is rendered OUTSIDE the Collapse
        so operators can enable/disable a whole group without first
        expanding it — this matters with 7+ collapsed-by-default cards.
        The Collapse then holds the per-template detail.
      */}
      {templates?.groups.map((group) => {
        const groupState = desired.groups[group.id] ?? { installed: false, rulesEnabled: {} };
        const isOpen = openGroups[group.id] ?? groupState.installed;
        return (
          <GroupRow<AlertTemplateDef>
            key={group.id}
            group={group}
            groupState={groupState}
            thresholds={desired.thresholds?.[group.id] ?? {}}
            isOpen={isOpen}
            onToggleInstall={(next) => onToggleGroupInstall(group.id, next)}
            onToggleOpen={(next) => setOpenGroups((prev) => ({ ...prev, [group.id]: next }))}
            onToggleRuleEnabled={(tplId, next) => onToggleRuleEnabled(group.id, tplId, next)}
            onThresholdChange={(tplId, key, value) => onThresholdChange(group.id, tplId, key, value)}
            renderTemplateMeta={renderAlertSeverity(s.severity)}
            emptyHint={groupEmptyHint}
            testIds={groupRowTestIds}
          />
        );
      })}

      <div className={s.footer}>
        <div>
          <strong>Labels:</strong> {footerLabels}
        </div>
        <div>
          <strong>Folder:</strong> {footerFolder}
        </div>
        <div className={s.footerHint}>{footerHint}</div>
      </div>

      <ConfirmModal
        isOpen={confirmUninstall}
        title={uninstallConfirmTitle}
        body={uninstallConfirmBody}
        confirmText="Uninstall"
        confirmVariant="destructive"
        onConfirm={onUninstallAll}
        onDismiss={() => setConfirmUninstall(false)}
        icon="trash-alt"
        modalClass={testIds.alertRulesPanel.uninstallConfirm}
      />
    </div>
  );
}

function renderAlertSeverity(className: string) {
  return function AlertSeverityMeta(template: AlertTemplateDef) {
    return <span className={className}>{template.severity}</span>;
  };
}

function computeCounts(
  templates: AlertsTemplatesResponse | null,
  status: AlertsStatusResponse | null,
  desired: DesiredStatePayload,
) {
  const totalGroups = templates?.groups.length ?? 0;
  const installedIndex = indexInstalled(status?.installed ?? []);
  let installedGroups = 0;
  for (const gid of Object.keys(installedIndex)) {
    if (Object.keys(installedIndex[gid]).length > 0) {
      installedGroups++;
    }
  }
  // Prefer desired-state counts once the user has touched the form; fall
  // back to live `status` for the pre-edit view.
  const userInstalledGroups = Object.values(desired.groups).filter((g) => g.installed).length;
  const installedRules = status?.installed.length ?? 0;
  return {
    totalGroups,
    installedGroups: userInstalledGroups || installedGroups,
    installedRules,
  };
}

function formatRelative(iso: string): string {
  const t = Date.parse(iso);
  if (Number.isNaN(t)) {
    return iso;
  }
  const deltaSec = Math.max(1, Math.floor((Date.now() - t) / 1000));
  if (deltaSec < 60) {
    return `${deltaSec}s ago`;
  }
  const deltaMin = Math.floor(deltaSec / 60);
  if (deltaMin < 60) {
    return `${deltaMin}m ago`;
  }
  const deltaHr = Math.floor(deltaMin / 60);
  if (deltaHr < 24) {
    return `${deltaHr}h ago`;
  }
  const deltaDay = Math.floor(deltaHr / 24);
  return `${deltaDay}d ago`;
}

async function postReconcile(
  path: string,
  desired: DesiredStatePayload,
): Promise<ReconcileResultResponse> {
  const obs = getBackendSrv().fetch<ReconcileResultResponse>({
    url: `/api/plugins/${PLUGIN_ID}/resources/${path}`,
    method: 'POST',
    data: desired,
    showErrorAlert: false,
  });
  const res = await lastValueFrom(obs);
  return res.data;
}

async function postUninstallAll(path: string): Promise<ReconcileResultResponse> {
  const obs = getBackendSrv().fetch<ReconcileResultResponse>({
    url: `/api/plugins/${PLUGIN_ID}/resources/${path}`,
    method: 'POST',
    showErrorAlert: false,
  });
  const res = await lastValueFrom(obs);
  return res.data;
}

const getStyles = (theme: GrafanaTheme2) => ({
  root: css`
    padding: ${theme.spacing(3)} 0;
    max-width: 960px;
  `,
  heading: css`
    margin: 0 0 ${theme.spacing(1)} 0;
  `,
  subtitle: css`
    margin: 0 0 ${theme.spacing(2)} 0;
    color: ${theme.colors.text.secondary};
  `,
  statusRow: css`
    display: flex;
    gap: ${theme.spacing(2)};
    margin: ${theme.spacing(2, 0)};
    align-items: center;
    flex-wrap: wrap;
  `,
  pill: css`
    padding: ${theme.spacing(0.5, 1.5)};
    border-radius: ${theme.shape.radius.pill};
    background: ${theme.colors.background.secondary};
    border: 1px solid ${theme.colors.border.weak};
    font-size: ${theme.typography.bodySmall.fontSize};
  `,
  pillMuted: css`
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
  `,
  severity: css`
    font-size: ${theme.typography.bodySmall.fontSize};
    color: ${theme.colors.text.secondary};
    text-transform: uppercase;
    letter-spacing: 0.04em;
  `,
  footer: css`
    margin-top: ${theme.spacing(3)};
    padding: ${theme.spacing(2)};
    border: 1px solid ${theme.colors.border.weak};
    border-radius: ${theme.shape.radius.default};
    font-size: ${theme.typography.bodySmall.fontSize};
    color: ${theme.colors.text.secondary};
    display: flex;
    flex-direction: column;
    gap: ${theme.spacing(0.5)};
  `,
  footerHint: css`
    margin-top: ${theme.spacing(0.5)};
  `,
});
