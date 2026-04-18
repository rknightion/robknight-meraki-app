import React from 'react';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { of } from 'rxjs';
import { AlertRulesPanel, detectDrift, indexInstalled } from './AlertRulesPanel';
import { testIds } from '../testIds';
import { AlertsStatusResponse, AlertsTemplatesResponse, InstalledRuleInfo } from './alertsTypes';

// -----------------------------------------------------------------------------
// Mock @grafana/runtime's getBackendSrv — all four endpoints answered by a
// single fetchMock so assertions can inspect per-URL calls.
// -----------------------------------------------------------------------------

const fetchMock = jest.fn();

jest.mock('@grafana/runtime', () => ({
  __esModule: true,
  getBackendSrv: () => ({ fetch: fetchMock }),
}));

function mockTemplates(res: AlertsTemplatesResponse) {
  return of({ data: res });
}
function mockStatus(res: AlertsStatusResponse) {
  return of({ data: res });
}

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
          thresholds: [
            { key: 'for', type: 'duration', default: '5m', label: 'For', help: 'Window' },
          ],
        },
      ],
    },
  ],
};

const emptyStatus: AlertsStatusResponse = {
  installed: [],
  grafanaReady: true,
};

function setupFetch(templatesRes: AlertsTemplatesResponse, statusRes: AlertsStatusResponse) {
  fetchMock.mockImplementation((req: { url: string; method?: string }) => {
    if (req.url.endsWith('/resources/alerts/templates')) {
      return mockTemplates(templatesRes);
    }
    if (req.url.endsWith('/resources/alerts/status')) {
      return mockStatus(statusRes);
    }
    if (req.url.endsWith('/resources/alerts/reconcile')) {
      return of({
        data: {
          created: ['a'],
          updated: [],
          deleted: [],
          failed: [],
          startedAt: '2026-01-01T00:00:00Z',
          finishedAt: '2026-01-01T00:00:01Z',
        },
      });
    }
    if (req.url.endsWith('/resources/alerts/uninstall-all')) {
      return of({
        data: {
          created: [],
          updated: [],
          deleted: ['meraki-availability-device-offline-org1'],
          failed: [],
          startedAt: '2026-01-01T00:00:00Z',
          finishedAt: '2026-01-01T00:00:01Z',
        },
      });
    }
    return of({ data: {} });
  });
}

beforeEach(() => {
  fetchMock.mockReset();
});

describe('AlertRulesPanel', () => {
  it('renders heading, subtitle, and one group card', async () => {
    setupFetch(minimalTemplates, emptyStatus);
    render(<AlertRulesPanel />);

    expect(screen.getByText('Bundled alert rules')).toBeInTheDocument();
    expect(screen.getByText(/Rules are managed by the Meraki plugin/)).toBeInTheDocument();

    await waitFor(() => {
      expect(screen.getByText(/Availability/)).toBeInTheDocument();
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

  it('toggles the group install checkbox and exposes per-rule rows', async () => {
    setupFetch(minimalTemplates, emptyStatus);
    render(<AlertRulesPanel />);

    const toggle = await screen.findByTestId(
      testIds.alertRulesPanel.groupInstallToggle('availability'),
    );
    // Initially uninstalled — rule row should be absent.
    expect(
      screen.queryByTestId(testIds.alertRulesPanel.ruleEnabled('availability', 'device-offline')),
    ).toBeNull();

    fireEvent.click(toggle);

    await waitFor(() => {
      expect(
        screen.getByTestId(testIds.alertRulesPanel.ruleEnabled('availability', 'device-offline')),
      ).toBeInTheDocument();
    });
  });

  it('posts the expected body when Reconcile is clicked', async () => {
    setupFetch(minimalTemplates, emptyStatus);
    render(<AlertRulesPanel />);

    // Install the group so desired state has a non-trivial body.
    const toggle = await screen.findByTestId(
      testIds.alertRulesPanel.groupInstallToggle('availability'),
    );
    fireEvent.click(toggle);

    const reconcileBtn = await screen.findByTestId(testIds.alertRulesPanel.reconcileButton);
    fireEvent.click(reconcileBtn);

    await waitFor(() => {
      const reconcileCalls = fetchMock.mock.calls.filter(
        (c) => typeof c[0].url === 'string' && c[0].url.endsWith('/resources/alerts/reconcile'),
      );
      expect(reconcileCalls.length).toBe(1);
      const body = reconcileCalls[0][0].data;
      expect(body.groups.availability.installed).toBe(true);
      expect(body.groups.availability.rulesEnabled['device-offline']).toBe(true);
      expect(body.thresholds?.availability?.['device-offline']?.for).toBe('5m');
    });

    await waitFor(() => {
      expect(screen.getByTestId(testIds.alertRulesPanel.resultBanner)).toBeInTheDocument();
    });
  });

  it('opens the confirm dialog when Uninstall all is clicked', async () => {
    const installed: InstalledRuleInfo = {
      groupId: 'availability',
      templateId: 'device-offline',
      orgId: 'org1',
      uid: 'meraki-availability-device-offline-org1',
      enabled: true,
    };
    setupFetch(minimalTemplates, { installed: [installed], grafanaReady: true });
    render(<AlertRulesPanel />);

    const btn = await screen.findByTestId(testIds.alertRulesPanel.uninstallButton);
    await waitFor(() => expect(btn).not.toBeDisabled());
    fireEvent.click(btn);

    await waitFor(() => {
      expect(screen.getByText(/This will delete every rule this plugin installed/)).toBeInTheDocument();
    });
  });
});

describe('indexInstalled', () => {
  it('groups installed rules by groupId and templateId', () => {
    const rows: InstalledRuleInfo[] = [
      { groupId: 'availability', templateId: 'device-offline', orgId: 'a', uid: '1', enabled: true },
      { groupId: 'availability', templateId: 'device-offline', orgId: 'b', uid: '2', enabled: false },
      { groupId: 'wan', templateId: 'uplink-down', orgId: 'a', uid: '3', enabled: true },
    ];
    const idx = indexInstalled(rows);
    expect(idx.availability['device-offline'].uid).toBe('2');
    expect(idx.wan['uplink-down'].uid).toBe('3');
  });
});

describe('detectDrift', () => {
  it('flags drift when a rule is enabled live but disabled in desired', () => {
    const status = {
      installed: [
        {
          groupId: 'availability',
          templateId: 'device-offline',
          orgId: 'a',
          uid: '1',
          enabled: true,
        },
      ],
    };
    const desired = {
      groups: {
        availability: {
          installed: true,
          rulesEnabled: { 'device-offline': false },
        },
      },
    };
    expect(detectDrift(status, desired)).toBe(true);
  });

  it('returns false when desired matches live', () => {
    const status = {
      installed: [
        {
          groupId: 'availability',
          templateId: 'device-offline',
          orgId: 'a',
          uid: '1',
          enabled: true,
        },
      ],
    };
    const desired = {
      groups: {
        availability: {
          installed: true,
          rulesEnabled: { 'device-offline': true },
        },
      },
    };
    expect(detectDrift(status, desired)).toBe(false);
  });
});
