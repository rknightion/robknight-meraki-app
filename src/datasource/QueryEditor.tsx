import React, { useMemo } from 'react';
import { QueryEditorProps } from '@grafana/data';
import { Combobox, InlineField, Input, MultiCombobox } from '@grafana/ui';
import { MerakiDataSource } from './datasource';
import { DEFAULT_MERAKI_QUERY, MerakiDSOptions, MerakiQuery, QueryKind } from './types';

type Props = QueryEditorProps<MerakiDataSource, MerakiQuery, MerakiDSOptions>;

// Option rows for the Kind picker. Combobox's value is a plain string, so we
// don't need a SelectableValue wrapper here — the string `value` matches
// QueryKind's string enum values directly.
const KIND_OPTIONS: Array<{ label: string; value: QueryKind; description?: string }> = [
  { label: 'Organizations', value: QueryKind.Organizations, description: 'List organizations visible to the API key.' },
  { label: 'Networks', value: QueryKind.Networks, description: 'List networks in an organization.' },
  { label: 'Devices', value: QueryKind.Devices, description: 'List devices in an organization.' },
  { label: 'Device status overview', value: QueryKind.DeviceStatusOverview, description: 'Aggregated online/alerting/offline/dormant counts.' },
  { label: 'Sensor readings (latest)', value: QueryKind.SensorReadingsLatest, description: 'Most recent sample per sensor / metric.' },
  { label: 'Sensor readings (history)', value: QueryKind.SensorReadingsHistory, description: 'Native timeseries of sensor metrics.' },
  { label: 'Configuration changes', value: QueryKind.ConfigurationChanges, description: 'Organization change log (who changed what, when). Supports networkId + adminId filters.' },
  { label: 'Device availability changes', value: QueryKind.DeviceAvailabilityChanges, description: 'Device state-transition feed (online/offline flaps). Additive to the current-state availabilities kind.' },
  { label: 'Clients (per network)', value: QueryKind.ClientsList, description: 'Fan-out /networks/{id}/clients across selected networks. Optional MAC filter via metrics[0].' },
  { label: 'Client lookup', value: QueryKind.ClientLookup, description: 'Org-wide /clients/search by MAC. Empty/not-found returns a notice instead of an error.' },
  { label: 'Client sessions', value: QueryKind.ClientSessions, description: 'Per-client wireless latency history. networkIds[0] = network, metrics[0] = client id (MAC or key).' },
];

const SENSOR_METRIC_OPTIONS: Array<{ label: string; value: string }> = [
  { label: 'Temperature', value: 'temperature' },
  { label: 'Humidity', value: 'humidity' },
  { label: 'Door', value: 'door' },
  { label: 'Water', value: 'water' },
  { label: 'CO₂', value: 'co2' },
  { label: 'PM2.5', value: 'pm25' },
  { label: 'TVOC', value: 'tvoc' },
  { label: 'Noise', value: 'noise' },
  { label: 'Battery', value: 'battery' },
  { label: 'Indoor air quality', value: 'indoorAirQuality' },
];

export function QueryEditor({ query, onChange, onRunQuery }: Props) {
  const effective = useMemo<MerakiQuery>(
    () => ({ ...DEFAULT_MERAKI_QUERY, ...query } as MerakiQuery),
    [query]
  );

  const showNetworkField = effective.kind === QueryKind.Networks || effective.kind === QueryKind.SensorReadingsHistory || effective.kind === QueryKind.SensorReadingsLatest;
  const showOrgField = effective.kind !== QueryKind.Organizations;
  const showMetricsField = effective.kind === QueryKind.SensorReadingsHistory || effective.kind === QueryKind.SensorReadingsLatest;

  const handleCommit = (next: Partial<MerakiQuery>) => {
    onChange({ ...effective, ...next });
    onRunQuery();
  };

  return (
    <div>
      <InlineField label="Kind" labelWidth={18} tooltip="What type of Meraki query to run.">
        <Combobox<QueryKind>
          options={KIND_OPTIONS}
          value={effective.kind}
          onChange={(opt) => handleCommit({ kind: opt?.value ?? QueryKind.Organizations })}
          width={32}
        />
      </InlineField>

      {showOrgField && (
        <InlineField label="Organization ID" labelWidth={18} tooltip="Meraki organization ID. Supports $org variables.">
          <Input
            width={32}
            value={effective.orgId ?? ''}
            placeholder="$org or literal ID"
            onChange={(e) => onChange({ ...effective, orgId: e.currentTarget.value })}
            onBlur={onRunQuery}
          />
        </InlineField>
      )}

      {showNetworkField && (
        <InlineField label="Network IDs" labelWidth={18} tooltip="Comma-separated network IDs; supports $network.">
          <Input
            width={48}
            value={(effective.networkIds ?? []).join(',')}
            placeholder="$network or comma-separated IDs"
            onChange={(e) =>
              onChange({
                ...effective,
                networkIds: e.currentTarget.value
                  .split(',')
                  .map((s) => s.trim())
                  .filter(Boolean),
              })
            }
            onBlur={onRunQuery}
          />
        </InlineField>
      )}

      {showMetricsField && (
        <InlineField label="Metrics" labelWidth={18} tooltip="One or more sensor metrics.">
          <MultiCombobox<string>
            options={SENSOR_METRIC_OPTIONS}
            value={(effective.metrics as string[] | undefined) ?? []}
            onChange={(selected) => {
              const metrics = selected
                .map((o) => o.value)
                .filter((v): v is string => Boolean(v));
              handleCommit({ metrics: metrics as MerakiQuery['metrics'] });
            }}
            width={48}
          />
        </InlineField>
      )}
    </div>
  );
}
