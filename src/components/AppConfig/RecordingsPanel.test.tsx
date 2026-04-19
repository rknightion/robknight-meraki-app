import React from 'react';
import { render, screen, waitFor } from '@testing-library/react';
import { of } from 'rxjs';
import { RecordingsPanel } from './RecordingsPanel';
import { testIds } from '../testIds';
import { RecordingsStatusResponse, RecordingsTemplatesResponse } from './recordingsTypes';

// -----------------------------------------------------------------------------
// Mock @grafana/runtime. Two surfaces need stubbing:
//   1. `getBackendSrv()` — answers all `/recordings/*` + settings endpoints
//      via a single fetchMock.
//   2. `DataSourcePicker` — render a plain marker span so the test runs in
//      JSDOM without pulling the full dataSourceSrv wiring. We expose a
//      hook for assertions on `filter` and `current`.
// -----------------------------------------------------------------------------

const fetchMock = jest.fn();
const dsPickerPropsMock = jest.fn();

jest.mock('@grafana/runtime', () => ({
  __esModule: true,
  getBackendSrv: () => ({ fetch: fetchMock }),
  DataSourcePicker: (props: Record<string, unknown>) => {
    dsPickerPropsMock(props);
    return <span data-testid="mock-ds-picker" />;
  },
}));

function mockTemplates(res: RecordingsTemplatesResponse) {
  return of({ data: res });
}
function mockStatus(res: RecordingsStatusResponse) {
  return of({ data: res });
}

const minimalTemplates: RecordingsTemplatesResponse = {
  groups: [
    {
      id: 'availability',
      displayName: 'Availability',
      templates: [
        {
          id: 'device-status-overview',
          groupId: 'availability',
          displayName: 'Device status overview',
          metric: 'meraki_device_status_count',
          thresholds: [
            { key: 'interval', type: 'duration', default: '1m', label: 'Interval' },
          ],
        },
      ],
    },
  ],
};

const emptyStatus: RecordingsStatusResponse = {
  installed: [],
  grafanaReady: true,
};

function setupFetch(templatesRes: RecordingsTemplatesResponse, statusRes: RecordingsStatusResponse) {
  fetchMock.mockImplementation((req: { url: string; method?: string }) => {
    if (req.url.endsWith('/resources/recordings/templates')) {
      return mockTemplates(templatesRes);
    }
    if (req.url.endsWith('/resources/recordings/status')) {
      return mockStatus(statusRes);
    }
    if (req.url.endsWith('/resources/recordings/reconcile')) {
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
    if (req.url.endsWith('/resources/recordings/uninstall-all')) {
      return of({
        data: {
          created: [],
          updated: [],
          deleted: ['meraki-rec-availability-device-status-overview-org1'],
          failed: [],
          startedAt: '2026-01-01T00:00:00Z',
          finishedAt: '2026-01-01T00:00:01Z',
        },
      });
    }
    if (req.url.endsWith(`/settings`)) {
      return of({ data: {} });
    }
    return of({ data: {} });
  });
}

beforeEach(() => {
  fetchMock.mockReset();
  dsPickerPropsMock.mockReset();
});

describe('RecordingsPanel', () => {
  it('renders the datasource picker at the top of the panel', async () => {
    setupFetch(minimalTemplates, emptyStatus);
    render(<RecordingsPanel />);

    expect(screen.getByText('Bundled recording rules')).toBeInTheDocument();
    expect(screen.getByTestId(testIds.recordingsPanel.datasourcePicker)).toBeInTheDocument();
    expect(screen.getByTestId('mock-ds-picker')).toBeInTheDocument();
  });

  it('filters the DataSourcePicker to Prometheus-family types', async () => {
    setupFetch(minimalTemplates, emptyStatus);
    render(<RecordingsPanel />);

    await waitFor(() => expect(dsPickerPropsMock).toHaveBeenCalled());
    const props = dsPickerPropsMock.mock.calls[dsPickerPropsMock.mock.calls.length - 1][0] as {
      filter: (ds: { type: string }) => boolean;
    };
    expect(typeof props.filter).toBe('function');
    // Whitelist — every listed type should pass.
    expect(props.filter({ type: 'prometheus' })).toBe(true);
    expect(props.filter({ type: 'grafana-amazonprometheus-datasource' })).toBe(true);
    expect(props.filter({ type: 'mimir' })).toBe(true);
    expect(props.filter({ type: 'cortex' })).toBe(true);
    // Non-Prom-family types must be rejected.
    expect(props.filter({ type: 'loki' })).toBe(false);
    expect(props.filter({ type: 'influxdb' })).toBe(false);
    expect(props.filter({ type: 'elasticsearch' })).toBe(false);
  });

  it('disables the reconcile button until a target datasource is picked', async () => {
    setupFetch(minimalTemplates, emptyStatus);
    render(<RecordingsPanel />);

    const reconcile = await screen.findByTestId(testIds.recordingsPanel.reconcileButton);
    expect(reconcile).toBeDisabled();
    // And the hint should prompt the user to pick one.
    expect(screen.getByTestId(testIds.recordingsPanel.datasourceHint)).toBeInTheDocument();
  });

  it('enables the reconcile button when jsonData already holds a target datasource UID', async () => {
    setupFetch(minimalTemplates, emptyStatus);
    render(
      <RecordingsPanel
        jsonData={{
          recordings: {
            targetDatasourceUid: 'prom-uid',
          },
        }}
      />,
    );

    const reconcile = await screen.findByTestId(testIds.recordingsPanel.reconcileButton);
    await waitFor(() => expect(reconcile).not.toBeDisabled());
    expect(screen.queryByTestId(testIds.recordingsPanel.datasourceHint)).toBeNull();
  });

  it('renders the feature-toggle banner when grafanaReady is false', async () => {
    setupFetch(minimalTemplates, { ...emptyStatus, grafanaReady: false });
    render(<RecordingsPanel />);

    await waitFor(() => {
      expect(
        screen.getByTestId(testIds.recordingsPanel.featureToggleBanner),
      ).toBeInTheDocument();
    });
    expect(screen.getByText(/externalServiceAccounts/)).toBeInTheDocument();
  });

  it('links "View in Grafana" to the recording-rules UI', async () => {
    setupFetch(minimalTemplates, emptyStatus);
    render(<RecordingsPanel />);

    const link = await screen.findByTestId(testIds.recordingsPanel.viewInGrafana);
    expect(link).toHaveAttribute('href', '/alerting/recording-rules');
  });
});
