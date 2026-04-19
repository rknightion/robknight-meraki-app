import React from 'react';
import { Field, Input, MultiCombobox } from '@grafana/ui';
import { ThresholdSchemaDef } from './types';

export type ThresholdInputProps = {
  schema: ThresholdSchemaDef;
  testId: string;
  value: unknown;
  onChange: (next: unknown) => void;
};

export function ThresholdInput({ schema, testId, value, onChange }: ThresholdInputProps) {
  const label = schema.label || schema.key;

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
