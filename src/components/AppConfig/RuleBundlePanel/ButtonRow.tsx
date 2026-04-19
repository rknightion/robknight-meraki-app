import React from 'react';
import { css } from '@emotion/css';
import { GrafanaTheme2 } from '@grafana/data';
import { Button, LinkButton, useStyles2 } from '@grafana/ui';

export type ButtonRowTestIds = {
  reconcileButton: string;
  uninstallButton: string;
  viewInGrafana: string;
};

export type ButtonRowProps = {
  onReconcile: () => void;
  onRequestUninstall: () => void;
  reconcileLabel?: string;
  reconcileBusyLabel?: string;
  uninstallLabel?: string;
  viewInGrafanaHref: string;
  viewInGrafanaLabel: string;
  actionInFlight: boolean;
  loading: boolean;
  grafanaReady: boolean;
  installedRules: number;
  reconcileDisabled?: boolean;
  testIds: ButtonRowTestIds;
};

export function ButtonRow({
  onReconcile,
  onRequestUninstall,
  reconcileLabel = 'Reconcile selected',
  reconcileBusyLabel = 'Working…',
  uninstallLabel = 'Uninstall all',
  viewInGrafanaHref,
  viewInGrafanaLabel,
  actionInFlight,
  loading,
  grafanaReady,
  installedRules,
  reconcileDisabled = false,
  testIds,
}: ButtonRowProps) {
  const s = useStyles2(getStyles);
  return (
    <div className={s.actions}>
      <Button
        type="button"
        variant="primary"
        onClick={onReconcile}
        disabled={actionInFlight || loading || !grafanaReady || reconcileDisabled}
        data-testid={testIds.reconcileButton}
      >
        {actionInFlight ? reconcileBusyLabel : reconcileLabel}
      </Button>
      <Button
        type="button"
        variant="destructive"
        onClick={onRequestUninstall}
        disabled={actionInFlight || loading || installedRules === 0}
        data-testid={testIds.uninstallButton}
      >
        {uninstallLabel}
      </Button>
      <LinkButton
        href={viewInGrafanaHref}
        target="_blank"
        rel="noreferrer"
        variant="secondary"
        icon="external-link-alt"
        data-testid={testIds.viewInGrafana}
      >
        {viewInGrafanaLabel}
      </LinkButton>
    </div>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  actions: css`
    display: flex;
    gap: ${theme.spacing(1)};
    margin-bottom: ${theme.spacing(2)};
    flex-wrap: wrap;
  `,
});
