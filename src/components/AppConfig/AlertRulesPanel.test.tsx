import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { of } from 'rxjs';
import { AlertRulesPanel } from './AlertRulesPanel';
import { testIds } from '../testIds';
import { AlertsStatusResponse, AlertsTemplatesResponse } from './alertsTypes';

const fetchMock = jest.fn();

jest.mock('@grafana/runtime', () => ({
  __esModule: true,
  getBackendSrv: () => ({ fetch: fetchMock }),
}));

const minimalTemplates: AlertsTemplatesResponse = {
  groups: [
    {
      id: 'availability',
      displayName: 'Availability',
      templates: [
        {
          id: 'device-offline',
          groupId: 'availability',
          displayName: 'Device offline',
          severity: 'critical',
          thresholds: [],
        },
      ],
    },
  ],
};

const emptyStatus: AlertsStatusResponse = { installed: [], grafanaReady: true };

function setupFetch(templatesRes: AlertsTemplatesResponse, statusRes: AlertsStatusResponse) {
  fetchMock.mockImplementation((req: { url: string }) => {
    if (req.url.endsWith('/resources/alerts/templates')) {
      return of({ data: templatesRes });
    }
    if (req.url.endsWith('/resources/alerts/status')) {
      return of({ data: statusRes });
    }
    return of({ data: {} });
  });
}

beforeEach(() => {
  fetchMock.mockReset();
});

describe('AlertRulesPanel skeleton', () => {
  it('renders heading, subtitle, and action row', async () => {
    setupFetch(minimalTemplates, emptyStatus);
    render(<AlertRulesPanel />);

    expect(screen.getByText('Bundled alert rules')).toBeInTheDocument();
    expect(screen.getByText(/Rules are managed by the Meraki plugin/)).toBeInTheDocument();
    expect(screen.getByTestId(testIds.alertRulesPanel.reconcileButton)).toBeInTheDocument();
    expect(screen.getByTestId(testIds.alertRulesPanel.uninstallButton)).toBeInTheDocument();
    expect(screen.getByTestId(testIds.alertRulesPanel.viewInGrafana)).toBeInTheDocument();

    await waitFor(() => {
      expect(screen.getByTestId(testIds.alertRulesPanel.statusPill)).toHaveTextContent(
        '1 groups available',
      );
    });
  });

  it('renders the feature-toggle banner when grafanaReady is false', async () => {
    setupFetch(minimalTemplates, { ...emptyStatus, grafanaReady: false });
    render(<AlertRulesPanel />);

    await waitFor(() => {
      expect(
        screen.getByTestId(testIds.alertRulesPanel.featureToggleBanner),
      ).toBeInTheDocument();
    });
    expect(screen.getByText(/externalServiceAccounts/)).toBeInTheDocument();
  });
});
