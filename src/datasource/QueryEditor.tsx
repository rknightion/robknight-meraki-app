import React, { useMemo } from 'react';
import { QueryEditorProps, SelectableValue } from '@grafana/data';
import { InlineField, Input, Select } from '@grafana/ui';
import { MerakiDataSource } from './datasource';
import { DEFAULT_MERAKI_QUERY, MerakiDSOptions, MerakiQuery, QueryKind } from './types';

type Props = QueryEditorProps<MerakiDataSource, MerakiQuery, MerakiDSOptions>;

const KIND_OPTIONS: Array<SelectableValue<QueryKind>> = [
  { label: 'Organizations', value: QueryKind.Organizations, description: 'List organizations visible to the API key.' },
  { label: 'Networks', value: QueryKind.Networks, description: 'List networks in an organization.' },
  { label: 'Devices', value: QueryKind.Devices, description: 'List devices in an organization.' },
  { label: 'Device status overview', value: QueryKind.DeviceStatusOverview, description: 'Aggregated online/alerting/offline/dormant counts.' },
  { label: 'Sensor readings (latest)', value: QueryKind.SensorReadingsLatest, description: 'Most recent sample per sensor / metric.' },
  { label: 'Sensor readings (history)', value: QueryKind.SensorReadingsHistory, description: 'Native timeseries of sensor metrics.' },
];

const SENSOR_METRIC_OPTIONS: Array<SelectableValue<string>> = [
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
        <Select
          options={KIND_OPTIONS}
          value={KIND_OPTIONS.find((o) => o.value === effective.kind) ?? KIND_OPTIONS[0]}
          onChange={(v) => handleCommit({ kind: (v.value as QueryKind) ?? QueryKind.Organizations })}
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
          <Select
            isMulti
            options={SENSOR_METRIC_OPTIONS}
            value={SENSOR_METRIC_OPTIONS.filter((o) =>
              (effective.metrics as string[] | undefined)?.includes(o.value as string)
            )}
            onChange={(v) => {
              const values = v as unknown as Array<SelectableValue<string>>;
              const metrics = values
                .map((s) => s.value)
                .filter((s): s is string => Boolean(s));
              handleCommit({ metrics: metrics as MerakiQuery['metrics'] });
            }}
            width={48}
          />
        </InlineField>
      )}
    </div>
  );
}
