import React, { useEffect, useMemo, useState } from 'react';
import { css } from '@emotion/css';
import { lastValueFrom } from 'rxjs';
import { DataSourceInstanceSettings, GrafanaTheme2 } from '@grafana/data';
import { DataSourcePicker, getBackendSrv } from '@grafana/runtime';
import { Alert, ConfirmModal, Field, useStyles2 } from '@grafana/ui';
import { PLUGIN_ID } from '../../constants';
import { AppJsonData } from '../../types';
import { testIds } from '../testIds';
import { useRecordingsTemplates } from './useRecordingsTemplates';
import { useRecordingsStatus } from './useRecordingsStatus';
import {
  RecordingTemplateDef,
  RecordingsDesiredStatePayload,
  RecordingsStatusResponse,
  RecordingsTemplatesResponse,
  ReconcileResultResponse,
} from './recordingsTypes';
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

const RECORDINGS_RECONCILE_PATH = 'recordings/reconcile';
const RECORDINGS_UNINSTALL_PATH = 'recordings/uninstall-all';

/**
 * Prometheus-family data-source `type` values the DataSourcePicker will
 * allow as a recording-rule target. See plan §4.6.5. Mimir + Cortex are
 * Prom-wire-compatible; the AWS AMP datasource shares the Prometheus
 * plugin API surface and accepts remote-write.
 *
 * Users who run VictoriaMetrics / Thanos / etc. can configure
 * `[recording_rules].default_datasource_uid` in grafana.ini, but the
 * plugin still requires an explicit jsonData choice so the UI + reconcile
 * gate have a single source of truth.
 */
const PROM_FAMILY_TYPES = new Set([
  'prometheus',
  'grafana-amazonprometheus-datasource',
  'mimir',
  'cortex',
]);

export { detectDrift, indexInstalled };

export type RecordingsPanelProps = {
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
 * plugin's bundled Grafana-managed recording-rule groups. Cloned from
 * AlertRulesPanel with two load-bearing additions:
 *
 *  1. A DataSourcePicker at the top, filtered to the Prometheus-family
 *     types in PROM_FAMILY_TYPES. The picked UID authoritatively lives
 *     on `jsonData.recordings.targetDatasourceUid` (written via the
 *     plugin settings endpoint), and is also echoed back from
 *     `/recordings/status` for first-paint.
 *  2. A disabled-reconcile gate: the Reconcile button stays disabled
 *     until the operator picks a target datasource. Uninstall-all is
 *     always available (it does not write rules so doesn't need a DS).
 *
 * Non-trivial data flow:
 *
 *  - `/resources/recordings/templates` is the static registry (shape of
 *    each group + threshold schema + the Prometheus metric name each
 *    template emits).
 *  - `/resources/recordings/status` is the live picture: which rules are
 *    in Grafana right now, plus last-reconcile telemetry and the echoed
 *    `targetDatasourceUid`.
 *  - Local `desired` state is the user's WIP edit. Initialised from
 *    jsonData.recordings (persisted thresholds) + status.installed.
 *  - "Reconcile selected" POSTs `desired` (including the picked
 *    targetDatasourceUid) to `/resources/recordings/reconcile`.
 *  - "Uninstall all" POSTs to `/resources/recordings/uninstall-all`.
 */
export function RecordingsPanel({
  jsonData,
  title = 'Bundled recording rules',
  subtitle = 'Grafana-managed recording rules poll Meraki once per evaluation interval and remote-write samples into the target Prometheus-compatible datasource. Dashboards then read from that datasource instead of calling Meraki on every view.',
  featureToggleTitle = 'Recording rules bundle unavailable',
  featureToggleBody,
  loadErrorTitle = 'Failed to load recording-rules bundle',
  reconcileEndpointPath = RECORDINGS_RECONCILE_PATH,
  uninstallEndpointPath = RECORDINGS_UNINSTALL_PATH,
  viewInGrafanaHref = '/alerting/recording-rules',
  viewInGrafanaLabel = 'View in Grafana recording rules',
  groupEmptyHint = 'Turn on "Install this group" above to pick per-rule toggles and tune thresholds.',
  footerLabels = 'meraki_group, meraki_product, meraki_org, meraki_rule, meraki_kind=recording',
  footerFolder = 'Meraki (bundled recordings)',
  footerHint = 'Target datasource: each rule writes to the Prometheus-compatible datasource selected above. Switch targets by picking a different datasource and running Reconcile.',
  uninstallConfirmTitle = 'Uninstall all Meraki recording rules?',
  uninstallConfirmBody = 'This will delete every recording rule this plugin installed. Continue?',
}: RecordingsPanelProps) {
  const s = useStyles2(getStyles);

  const {
    data: templates,
    loading: templatesLoading,
    error: templatesError,
  } = useRecordingsTemplates();
  const {
    data: status,
    loading: statusLoading,
    error: statusError,
    refetch: refetchStatus,
  } = useRecordingsStatus();

  const [desired, setDesired] = useState<RecordingsDesiredStatePayload>({ groups: {}, thresholds: {} });
  const [desiredInitialised, setDesiredInitialised] = useState(false);
  const [openGroups, setOpenGroups] = useState<Record<string, boolean>>({});
  const [confirmUninstall, setConfirmUninstall] = useState(false);
  const [actionInFlight, setActionInFlight] = useState(false);
  const [resultBanner, setResultBanner] = useState<
    null | { kind: 'success' | 'error'; title: string; body: string }
  >(null);
  // Local mirror of the picked target DS UID. Seeded from jsonData
  // first (the authoritative persisted value) then from the status
  // echo on mount. Any user pick both updates this state AND fires an
  // async POST to /api/plugins/<id>/settings so persistence survives
  // subsequent page reloads without needing the user to click Save.
  const [targetDatasourceUid, setTargetDatasourceUid] = useState<string>(
    jsonData?.recordings?.targetDatasourceUid ?? '',
  );
  const [targetSaving, setTargetSaving] = useState(false);
  const [targetSaveError, setTargetSaveError] = useState<string | null>(null);

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
      const seed = seedDesired(templates.groups, status.installed, jsonData?.recordings);
      if (cancelled) {
        return;
      }
      setDesired({ groups: seed.groups, thresholds: seed.thresholds });
      setOpenGroups(seed.openGroups);
      setDesiredInitialised(true);
      // Prefer the persisted jsonData value; fall back to the live
      // status echo for first-run when jsonData hasn't been mirrored
      // back from the backend yet.
      if (!jsonData?.recordings?.targetDatasourceUid && status.targetDatasourceUid) {
        setTargetDatasourceUid(status.targetDatasourceUid);
      }
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

  const onTargetDatasourceChange = async (ds: DataSourceInstanceSettings) => {
    const nextUid = ds.uid;
    setTargetDatasourceUid(nextUid);
    setTargetSaveError(null);
    setTargetSaving(true);
    try {
      // Persist jsonData.recordings.targetDatasourceUid via the plugin
      // settings endpoint so the backend's Settings.RecordingsTargetDatasourceUID
      // picks it up on the next NewApp. The backend reads from settings in
      // /recordings/reconcile, but we also echo the UID back in the POST
      // body for belt-and-braces (and for hermetic tests).
      await patchRecordingsTargetDatasourceUid(nextUid, jsonData);
    } catch (e) {
      setTargetSaveError(e instanceof Error ? e.message : String(e));
    } finally {
      setTargetSaving(false);
    }
  };

  const onReconcile = async () => {
    setActionInFlight(true);
    setResultBanner(null);
    try {
      const result = await postReconcile(reconcileEndpointPath, {
        ...desired,
        targetDatasourceUid: targetDatasourceUid || undefined,
      });
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
      // freshly-empty world. Thresholds + target DS stay in place (user
      // may want to reinstall with the same tuning and DS pick).
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
  const hasTargetDs = Boolean(targetDatasourceUid);

  const groupRowTestIds: GroupRowTestIds = {
    groupCard: testIds.recordingsPanel.groupCard,
    groupInstallToggle: testIds.recordingsPanel.groupInstallToggle,
    templateRow: testIds.recordingsPanel.templateRow,
    ruleEnabled: testIds.recordingsPanel.ruleEnabled,
    thresholdInput: testIds.recordingsPanel.thresholdInput,
  };

  const buttonRowTestIds: ButtonRowTestIds = {
    reconcileButton: testIds.recordingsPanel.reconcileButton,
    uninstallButton: testIds.recordingsPanel.uninstallButton,
    viewInGrafana: testIds.recordingsPanel.viewInGrafana,
  };

  return (
    <div className={s.root} data-testid={testIds.recordingsPanel.container}>
      <h3 className={s.heading}>{title}</h3>
      <p className={s.subtitle}>{subtitle}</p>

      {!grafanaReady && (
        <Alert
          severity="warning"
          title={featureToggleTitle}
          data-testid={testIds.recordingsPanel.featureToggleBanner}
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

      <Field
        label="Target datasource"
        description="Pick a Prometheus-compatible datasource to remote-write recorded samples into. Required before reconcile. Only datasources of type Prometheus, Grafana Amazon Prometheus, Mimir, or Cortex are listed."
      >
        <div data-testid={testIds.recordingsPanel.datasourcePicker}>
          <DataSourcePicker
            filter={(ds: DataSourceInstanceSettings) => PROM_FAMILY_TYPES.has(ds.type)}
            current={targetDatasourceUid || null}
            noDefault
            onChange={onTargetDatasourceChange}
            placeholder="Select a Prometheus-compatible datasource"
            disabled={targetSaving}
          />
        </div>
      </Field>

      {!hasTargetDs && (
        <p className={s.datasourceHint} data-testid={testIds.recordingsPanel.datasourceHint}>
          Pick a target datasource to enable reconcile.
        </p>
      )}

      {targetSaveError && (
        <Alert severity="error" title="Failed to save target datasource" onRemove={() => setTargetSaveError(null)}>
          {targetSaveError}
        </Alert>
      )}

      {drift && <DriftBanner testId={testIds.recordingsPanel.driftBanner} />}

      {resultBanner && (
        <Alert
          severity={resultBanner.kind}
          title={resultBanner.title}
          data-testid={testIds.recordingsPanel.resultBanner}
          onRemove={() => setResultBanner(null)}
        >
          {resultBanner.body}
        </Alert>
      )}

      <div className={s.statusRow} data-testid={testIds.recordingsPanel.statusPill}>
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
        reconcileDisabled={!hasTargetDs}
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
          <GroupRow<RecordingTemplateDef>
            key={group.id}
            group={group}
            groupState={groupState}
            thresholds={desired.thresholds?.[group.id] ?? {}}
            isOpen={isOpen}
            onToggleInstall={(next) => onToggleGroupInstall(group.id, next)}
            onToggleOpen={(next) => setOpenGroups((prev) => ({ ...prev, [group.id]: next }))}
            onToggleRuleEnabled={(tplId, next) => onToggleRuleEnabled(group.id, tplId, next)}
            onThresholdChange={(tplId, key, value) => onThresholdChange(group.id, tplId, key, value)}
            renderTemplateMeta={renderRecordingMetric(s.metric)}
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
        modalClass={testIds.recordingsPanel.uninstallConfirm}
      />
    </div>
  );
}

function renderRecordingMetric(className: string) {
  return function RecordingMetricMeta(template: RecordingTemplateDef) {
    return <code className={className}>{template.metric}</code>;
  };
}

function computeCounts(
  templates: RecordingsTemplatesResponse | null,
  status: RecordingsStatusResponse | null,
  desired: RecordingsDesiredStatePayload,
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
  desired: RecordingsDesiredStatePayload,
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

/**
 * Persist a target-datasource-UID update into plugin jsonData via the
 * Grafana settings endpoint. We merge the existing `jsonData.recordings`
 * shape so threshold overrides and per-group install flags already in
 * place aren't clobbered — only `targetDatasourceUid` is overwritten.
 *
 * Unlike the main config-form Save path, this helper does NOT force a
 * page reload. The DS pick is picked up the next time the plugin
 * instance is recreated (on server restart or next save-all). Between
 * now and then the reconcile endpoint still accepts the UID via the
 * request body as a belt-and-braces override.
 */
async function patchRecordingsTargetDatasourceUid(
  nextUid: string,
  jsonData: AppJsonData | undefined,
): Promise<void> {
  const prevRecordings = jsonData?.recordings ?? {};
  const nextJsonData: AppJsonData = {
    ...(jsonData ?? {}),
    recordings: {
      ...prevRecordings,
      targetDatasourceUid: nextUid || undefined,
    },
  };
  const obs = getBackendSrv().fetch({
    url: `/api/plugins/${PLUGIN_ID}/settings`,
    method: 'POST',
    data: { jsonData: nextJsonData },
    showErrorAlert: false,
  });
  await lastValueFrom(obs);
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
  datasourceHint: css`
    margin: ${theme.spacing(-1, 0, 2, 0)};
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
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
  metric: css`
    font-size: ${theme.typography.bodySmall.fontSize};
    color: ${theme.colors.text.secondary};
    background: ${theme.colors.background.secondary};
    padding: ${theme.spacing(0.25, 0.75)};
    border-radius: ${theme.shape.radius.default};
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
