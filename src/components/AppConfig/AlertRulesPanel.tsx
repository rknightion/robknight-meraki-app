import React, { useEffect, useMemo, useState } from 'react';
import { css } from '@emotion/css';
import { lastValueFrom } from 'rxjs';
import { GrafanaTheme2 } from '@grafana/data';
import { getBackendSrv } from '@grafana/runtime';
import {
  Alert,
  Button,
  Checkbox,
  Collapse,
  ConfirmModal,
  Field,
  Input,
  LinkButton,
  MultiCombobox,
  useStyles2,
} from '@grafana/ui';
import { PLUGIN_ID } from '../../constants';
import { AlertsConfig, AppJsonData } from '../../types';
import { testIds } from '../testIds';
import { useAlertsTemplates } from './useAlertsTemplates';
import { useAlertsStatus } from './useAlertsStatus';
import {
  AlertGroupDef,
  AlertsGroupStateDto,
  AlertTemplateDef,
  DesiredStatePayload,
  InstalledRuleInfo,
  ReconcileResultResponse,
  ThresholdSchemaDef,
} from './alertsTypes';

export type AlertRulesPanelProps = {
  jsonData?: AppJsonData;
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
export function AlertRulesPanel({ jsonData }: AlertRulesPanelProps) {
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
      const result = await postReconcile(desired);
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
      const result = await postUninstallAll();
      setResultBanner({
        kind: 'success',
        title: 'Uninstall complete',
        body: `Deleted ${result.deleted.length} rule(s).`,
      });
      // Clear local desired-state install flags so the UI reflects the
      // freshly-empty world. Thresholds stay in place (user may want to
      // reinstall with the same tuning).
      setDesired((prev) => {
        const next: Record<string, AlertsGroupStateDto> = {};
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

  return (
    <div className={s.root} data-testid={testIds.alertRulesPanel.container}>
      <h3 className={s.heading}>Bundled alert rules</h3>
      <p className={s.subtitle}>
        Rules are managed by the Meraki plugin. Contact points and notification policies are your
        responsibility — see the Grafana Alerting UI.
      </p>

      {!grafanaReady && (
        <Alert
          severity="warning"
          title="Alerts bundle unavailable"
          data-testid={testIds.alertRulesPanel.featureToggleBanner}
        >
          Enable the <code>externalServiceAccounts</code> feature toggle in Grafana (or upgrade to a
          build where it is on by default), then reload this page.
        </Alert>
      )}

      {loadError && (
        <Alert severity="error" title="Failed to load alert bundle">
          {loadError}
        </Alert>
      )}

      {drift && (
        <Alert
          severity="info"
          title="External edits detected"
          data-testid={testIds.alertRulesPanel.driftBanner}
        >
          One or more managed rules differ from the desired state configured here. Running
          reconcile will revert them.
        </Alert>
      )}

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

      <div className={s.actions}>
        <Button
          type="button"
          variant="primary"
          onClick={onReconcile}
          disabled={actionInFlight || loading || !grafanaReady}
          data-testid={testIds.alertRulesPanel.reconcileButton}
        >
          {actionInFlight ? 'Working…' : 'Reconcile selected'}
        </Button>
        <Button
          type="button"
          variant="destructive"
          onClick={() => setConfirmUninstall(true)}
          disabled={actionInFlight || loading || counts.installedRules === 0}
          data-testid={testIds.alertRulesPanel.uninstallButton}
        >
          Uninstall all
        </Button>
        <LinkButton
          href="/alerting/grouped?dataSource=grafana"
          target="_blank"
          rel="noreferrer"
          variant="secondary"
          icon="external-link-alt"
          data-testid={testIds.alertRulesPanel.viewInGrafana}
        >
          View in Grafana Alerting
        </LinkButton>
      </div>

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
          <div
            key={group.id}
            className={s.groupCard}
            data-testid={testIds.alertRulesPanel.groupCard(group.id)}
          >
            <div className={s.groupHeader}>
              <Checkbox
                label={`${group.displayName} (${group.templates.length} rule${group.templates.length === 1 ? '' : 's'})`}
                value={groupState.installed}
                onChange={(e) =>
                  onToggleGroupInstall(
                    group.id,
                    (e.currentTarget as HTMLInputElement).checked,
                  )
                }
                data-testid={testIds.alertRulesPanel.groupInstallToggle(group.id)}
              />
            </div>
            <Collapse
              label={<span className={s.groupMeta}>Rule detail</span>}
              isOpen={isOpen}
              onToggle={(next) => setOpenGroups((prev) => ({ ...prev, [group.id]: next }))}
            >
              <div className={s.groupBody}>
                {groupState.installed ? (
                  <div className={s.templateList}>
                    {group.templates.map((tpl) => (
                      <TemplateRow
                        key={tpl.id}
                        group={group}
                        template={tpl}
                        enabled={groupState.rulesEnabled[tpl.id] ?? true}
                        thresholds={desired.thresholds?.[group.id]?.[tpl.id] ?? {}}
                        onEnabledChange={(next) =>
                          onToggleRuleEnabled(group.id, tpl.id, next)
                        }
                        onThresholdChange={(key, value) =>
                          onThresholdChange(group.id, tpl.id, key, value)
                        }
                      />
                    ))}
                  </div>
                ) : (
                  <p className={s.groupHint}>
                    Turn on &quot;Install this group&quot; above to pick per-rule toggles and
                    tune thresholds.
                  </p>
                )}
              </div>
            </Collapse>
          </div>
        );
      })}

      <div className={s.footer}>
        <div>
          <strong>Labels:</strong> severity, meraki_group, meraki_product, meraki_org, meraki_rule
        </div>
        <div>
          <strong>Folder:</strong> Meraki (bundled)
        </div>
        <div className={s.footerHint}>
          Routing: use Grafana&apos;s notification-policy matchers on the labels above to send
          these rules to the right contact points.
        </div>
      </div>

      <ConfirmModal
        isOpen={confirmUninstall}
        title="Uninstall all Meraki alert rules?"
        body="This will delete every rule this plugin installed. Continue?"
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

// -----------------------------------------------------------------------------
// Sub-component: one template row with per-threshold editors.
// -----------------------------------------------------------------------------

type TemplateRowProps = {
  group: AlertGroupDef;
  template: AlertTemplateDef;
  enabled: boolean;
  thresholds: Record<string, unknown>;
  onEnabledChange: (next: boolean) => void;
  onThresholdChange: (key: string, value: unknown) => void;
};

function TemplateRow({
  group,
  template,
  enabled,
  thresholds,
  onEnabledChange,
  onThresholdChange,
}: TemplateRowProps) {
  const s = useStyles2(getStyles);
  return (
    <div
      className={s.templateRow}
      data-testid={testIds.alertRulesPanel.templateRow(group.id, template.id)}
    >
      <div className={s.templateHeader}>
        <Checkbox
          value={enabled}
          onChange={(e) => onEnabledChange((e.currentTarget as HTMLInputElement).checked)}
          label={template.displayName}
          data-testid={testIds.alertRulesPanel.ruleEnabled(group.id, template.id)}
        />
        <span className={s.severity}>{template.severity}</span>
      </div>

      {template.thresholds.length > 0 && (
        <div className={s.thresholdGrid}>
          {template.thresholds.map((schema) => (
            <ThresholdInput
              key={schema.key}
              schema={schema}
              groupId={group.id}
              templateId={template.id}
              value={thresholds[schema.key]}
              onChange={(next) => onThresholdChange(schema.key, next)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

type ThresholdInputProps = {
  schema: ThresholdSchemaDef;
  groupId: string;
  templateId: string;
  value: unknown;
  onChange: (next: unknown) => void;
};

function ThresholdInput({ schema, groupId, templateId, value, onChange }: ThresholdInputProps) {
  const label = schema.label || schema.key;
  const testId = testIds.alertRulesPanel.thresholdInput(groupId, templateId, schema.key);

  switch (schema.type) {
    case 'list': {
      const options = (schema.options ?? []).map((o) => ({ label: o, value: o }));
      const selected = Array.isArray(value) ? (value as string[]) : [];
      return (
        <Field label={label} description={schema.help}>
          <MultiCombobox<string>
            options={options}
            value={selected}
            onChange={(items) => {
              const next = items
                .map((it) => it.value)
                .filter((v): v is string => typeof v === 'string');
              onChange(next);
            }}
            width={30}
            data-testid={testId}
          />
        </Field>
      );
    }
    case 'int':
    case 'float': {
      const str = value === undefined || value === null ? '' : String(value);
      return (
        <Field label={label} description={schema.help}>
          <Input
            type="number"
            value={str}
            onChange={(e) => {
              const raw = (e.currentTarget as HTMLInputElement).value;
              if (raw === '') {
                onChange(undefined);
                return;
              }
              const n = Number(raw);
              onChange(Number.isFinite(n) ? n : raw);
            }}
            width={20}
            data-testid={testId}
          />
        </Field>
      );
    }
    case 'duration':
    case 'string':
    default: {
      const str = value === undefined || value === null ? '' : String(value);
      return (
        <Field label={label} description={schema.help}>
          <Input
            value={str}
            onChange={(e) => onChange((e.currentTarget as HTMLInputElement).value)}
            width={20}
            data-testid={testId}
            placeholder={schema.type === 'duration' ? 'e.g. 5m' : undefined}
          />
        </Field>
      );
    }
  }
}

// -----------------------------------------------------------------------------
// Pure helpers (exported for tests).
// -----------------------------------------------------------------------------

/**
 * Pure seeder used by the first-render init effect. Splitting this out of
 * the component keeps the effect's body free of setState sequences (lint:
 * `react-hooks/set-state-in-effect`) and makes the initialisation logic
 * unit-testable in isolation.
 *
 * Precedence for every field:
 *   1. If the user has a persisted `AppJsonData.alerts` value → use it.
 *   2. Else fall back to live status (installed → installed=true,
 *      enabled flag mirrored from the live row).
 *   3. Else fall back to template defaults.
 */
export function seedDesired(
  groups: AlertGroupDef[],
  installed: InstalledRuleInfo[],
  saved: AlertsConfig | undefined,
): {
  groups: Record<string, AlertsGroupStateDto>;
  thresholds: Record<string, Record<string, Record<string, unknown>>>;
  openGroups: Record<string, boolean>;
} {
  const savedGroups = saved?.groups ?? {};
  const savedThresholds = saved?.thresholds ?? {};
  const installedIndex = indexInstalled(installed);

  const nextGroups: Record<string, AlertsGroupStateDto> = {};
  const nextThresholds: Record<string, Record<string, Record<string, unknown>>> = {};
  const openGroups: Record<string, boolean> = {};

  for (const group of groups) {
    const savedGroup = savedGroups[group.id];
    const installedUnderGroup = installedIndex[group.id] ?? {};
    const hasAnyInstalled = Object.keys(installedUnderGroup).length > 0;

    const rulesEnabled: Record<string, boolean> = {};
    for (const tpl of group.templates) {
      if (savedGroup?.rulesEnabled && tpl.id in savedGroup.rulesEnabled) {
        rulesEnabled[tpl.id] = Boolean(savedGroup.rulesEnabled[tpl.id]);
      } else if (installedUnderGroup[tpl.id]) {
        rulesEnabled[tpl.id] = installedUnderGroup[tpl.id].enabled;
      } else {
        rulesEnabled[tpl.id] = true;
      }
    }

    nextGroups[group.id] = {
      installed: savedGroup?.installed ?? hasAnyInstalled,
      rulesEnabled,
    };
    openGroups[group.id] = nextGroups[group.id].installed;

    const groupThresholds: Record<string, Record<string, unknown>> = {};
    for (const tpl of group.templates) {
      const perTpl: Record<string, unknown> = {};
      const savedPerTpl = savedThresholds[group.id]?.[tpl.id] ?? {};
      for (const th of tpl.thresholds) {
        if (th.key in savedPerTpl) {
          perTpl[th.key] = savedPerTpl[th.key];
        } else if (th.default !== undefined) {
          perTpl[th.key] = th.default;
        }
      }
      if (Object.keys(perTpl).length > 0) {
        groupThresholds[tpl.id] = perTpl;
      }
    }
    if (Object.keys(groupThresholds).length > 0) {
      nextThresholds[group.id] = groupThresholds;
    }
  }

  return { groups: nextGroups, thresholds: nextThresholds, openGroups };
}

export function indexInstalled(
  installed: InstalledRuleInfo[],
): Record<string, Record<string, InstalledRuleInfo>> {
  const out: Record<string, Record<string, InstalledRuleInfo>> = {};
  for (const row of installed) {
    if (!row.groupId || !row.templateId) {
      continue;
    }
    const bucket = (out[row.groupId] = out[row.groupId] ?? {});
    // When a group+template is installed across multiple orgs we only
    // retain one row for the "is this installed?" decision; enabled state
    // collapses to the last-seen row, which matches how the UI presents a
    // single enable toggle per template.
    bucket[row.templateId] = row;
  }
  return out;
}

function computeCounts(
  templates: { groups: AlertGroupDef[] } | null,
  status: { installed: InstalledRuleInfo[] } | null,
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

export function detectDrift(
  status: { installed: InstalledRuleInfo[] } | null,
  desired: DesiredStatePayload,
): boolean {
  if (!status) {
    return false;
  }
  for (const row of status.installed) {
    const groupState = desired.groups[row.groupId];
    if (!groupState) {
      // Rule lives in Grafana but the user hasn't loaded/chosen the group
      // yet — ignore rather than render a scary banner on first render.
      continue;
    }
    const wantEnabled = groupState.rulesEnabled[row.templateId] ?? true;
    const wantInstalled = groupState.installed;
    if (!wantInstalled) {
      // User wants this group uninstalled but rules still live — drift.
      return true;
    }
    if (wantEnabled !== row.enabled) {
      return true;
    }
  }
  return false;
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

async function postReconcile(desired: DesiredStatePayload): Promise<ReconcileResultResponse> {
  const obs = getBackendSrv().fetch<ReconcileResultResponse>({
    url: `/api/plugins/${PLUGIN_ID}/resources/alerts/reconcile`,
    method: 'POST',
    data: desired,
    showErrorAlert: false,
  });
  const res = await lastValueFrom(obs);
  return res.data;
}

async function postUninstallAll(): Promise<ReconcileResultResponse> {
  const obs = getBackendSrv().fetch<ReconcileResultResponse>({
    url: `/api/plugins/${PLUGIN_ID}/resources/alerts/uninstall-all`,
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
  actions: css`
    display: flex;
    gap: ${theme.spacing(1)};
    margin-bottom: ${theme.spacing(2)};
    flex-wrap: wrap;
  `,
  groupBody: css`
    padding: ${theme.spacing(1, 2, 2, 2)};
  `,
  groupMeta: css`
    color: ${theme.colors.text.secondary};
    font-weight: normal;
  `,
  groupToggleRow: css`
    margin-bottom: ${theme.spacing(1.5)};
  `,
  groupCard: css`
    margin-bottom: ${theme.spacing(2)};
    border: 1px solid ${theme.colors.border.weak};
    border-radius: ${theme.shape.radius.default};
    background: ${theme.colors.background.primary};
  `,
  groupHeader: css`
    padding: ${theme.spacing(1.5, 2)};
    border-bottom: 1px solid ${theme.colors.border.weak};
  `,
  groupHint: css`
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
    margin: 0;
  `,
  templateList: css`
    display: flex;
    flex-direction: column;
    gap: ${theme.spacing(2)};
  `,
  templateRow: css`
    padding: ${theme.spacing(1.5)};
    border: 1px solid ${theme.colors.border.weak};
    border-radius: ${theme.shape.radius.default};
    background: ${theme.colors.background.secondary};
  `,
  templateHeader: css`
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: ${theme.spacing(2)};
    margin-bottom: ${theme.spacing(1)};
  `,
  severity: css`
    font-size: ${theme.typography.bodySmall.fontSize};
    color: ${theme.colors.text.secondary};
    text-transform: uppercase;
    letter-spacing: 0.04em;
  `,
  thresholdGrid: css`
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: ${theme.spacing(1, 2)};
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
