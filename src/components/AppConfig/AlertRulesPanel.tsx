import React from 'react';
import { css } from '@emotion/css';
import { GrafanaTheme2 } from '@grafana/data';
import { Alert, Button, LinkButton, useStyles2 } from '@grafana/ui';
import { AppJsonData } from '../../types';
import { testIds } from '../testIds';
import { useAlertsTemplates } from './useAlertsTemplates';
import { useAlertsStatus } from './useAlertsStatus';

export type AlertRulesPanelProps = {
  jsonData?: AppJsonData;
};

/**
 * Phase-4 skeleton of the Bundled alert rules configuration section. This
 * initial revision wires the two read-side hooks and renders the static
 * chrome — heading, subtitle, feature-toggle banner, status pill, and the
 * action row — without the group-cards/threshold-editor body. The next
 * commit expands this file with the full per-group install toggles,
 * threshold editor, reconcile/uninstall actions, and drift detection.
 */
export function AlertRulesPanel(_props: AlertRulesPanelProps) {
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
  } = useAlertsStatus();

  const loading = templatesLoading || statusLoading;
  const loadError = templatesError ?? statusError;
  const grafanaReady = status?.grafanaReady ?? true;

  const totalGroups = templates?.groups.length ?? 0;
  const installedRules = status?.installed.length ?? 0;

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

      <div className={s.statusRow} data-testid={testIds.alertRulesPanel.statusPill}>
        <span className={s.pill}>
          {totalGroups} groups available · {installedRules} rules installed
        </span>
      </div>

      <div className={s.actions}>
        <Button
          type="button"
          variant="primary"
          disabled
          data-testid={testIds.alertRulesPanel.reconcileButton}
        >
          Reconcile selected
        </Button>
        <Button
          type="button"
          variant="destructive"
          disabled
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

      {loading && <p className={s.subtitle}>Loading bundle…</p>}
    </div>
  );
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
  actions: css`
    display: flex;
    gap: ${theme.spacing(1)};
    margin-bottom: ${theme.spacing(2)};
    flex-wrap: wrap;
  `,
});
