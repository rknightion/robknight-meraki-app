import React, { ChangeEvent, useState } from 'react';
import { css } from '@emotion/css';
import { lastValueFrom } from 'rxjs';
import {
  AppPluginMeta,
  GrafanaTheme2,
  PluginConfigPageProps,
  PluginMeta,
} from '@grafana/data';
import { getBackendSrv } from '@grafana/runtime';
import {
  Alert,
  Button,
  Field,
  FieldSet,
  Input,
  SecretInput,
  useStyles2,
} from '@grafana/ui';
import { testIds } from '../testIds';
import { AppJsonData, AppSecureJsonData } from '../../types';
import { DEFAULT_MERAKI_BASE_URL, PLUGIN_ID } from '../../constants';

export type AppConfigProps = PluginConfigPageProps<AppPluginMeta<AppJsonData>>;

type FormState = {
  baseUrl: string;
  sharedFraction: string;
  apiKey: string;
  isApiKeySet: boolean;
};

type HealthResult = { status: 'ok' | 'error'; message: string } | null;
type SaveState = null | { ok: true; message: string } | { ok: false; message: string };

// Keep the "saved successfully, reloading…" banner on screen briefly so the user sees
// confirmation before the full-page reload swaps the DOM out from under them.
const RELOAD_DELAY_MS = 600;

const AppConfig = ({ plugin }: AppConfigProps) => {
  const s = useStyles2(getStyles);
  const { enabled, pinned, jsonData } = plugin.meta;

  const [form, setForm] = useState<FormState>({
    baseUrl: jsonData?.baseUrl ?? '',
    sharedFraction:
      jsonData?.sharedFraction !== undefined ? String(jsonData.sharedFraction) : '',
    apiKey: '',
    isApiKeySet: Boolean(jsonData?.isApiKeySet),
  });
  const [health, setHealth] = useState<HealthResult>(null);
  const [saveState, setSaveState] = useState<SaveState>(null);
  const [saving, setSaving] = useState(false);
  const [testing, setTesting] = useState(false);

  const onResetApiKey = () =>
    setForm((prev) => ({ ...prev, apiKey: '', isApiKeySet: false }));

  const onChange =
    (field: keyof FormState) => (event: ChangeEvent<HTMLInputElement>) => {
      setForm((prev) => ({ ...prev, [field]: event.target.value }));
    };

  const parsedSharedFraction = parseSharedFraction(form.sharedFraction);
  const sharedFractionInvalid =
    form.sharedFraction !== '' && parsedSharedFraction === null;

  // Save is disabled only when:
  //  - a save is already in flight, or
  //  - the sharedFraction input has a value that doesn't parse, or
  //  - there's nothing to save (no new key AND no previously-saved key).
  const nothingToSave = !form.isApiKeySet && !form.apiKey;
  const isSubmitDisabled = saving || sharedFractionInvalid || nothingToSave;

  // Test Connection is useful whenever the backend believes it has a key.
  const canTestConnection = form.isApiKeySet && !saving;

  const doSave = async () => {
    setSaving(true);
    setSaveState(null);
    setHealth(null);
    try {
      const nextJsonData: AppJsonData = {
        baseUrl: form.baseUrl || undefined,
        sharedFraction: parsedSharedFraction ?? undefined,
        // Mark the key as set if the user is submitting a new value OR it was already set.
        isApiKeySet: form.isApiKeySet || Boolean(form.apiKey),
      };
      const secureJsonData: AppSecureJsonData | undefined = form.apiKey
        ? { merakiApiKey: form.apiKey }
        : undefined;

      await updatePlugin(PLUGIN_ID, {
        enabled,
        pinned,
        jsonData: nextJsonData,
        secureJsonData,
      });

      // Optimistic update so the button state flips before the full reload.
      setForm((prev) => ({ ...prev, apiKey: '', isApiKeySet: true }));
      setSaveState({ ok: true, message: 'Saved. Reloading to pick up the new settings…' });

      // Grafana needs a full page reload for the backend plugin instance to be recreated
      // with the new secureJsonData; SPA navigation (locationService.reload) leaves the
      // React tree's plugin.meta stale, which is what caused the "nothing happens" UX.
      window.setTimeout(() => {
        window.location.reload();
      }, RELOAD_DELAY_MS);
    } catch (e) {
      setSaveState({ ok: false, message: 'Save failed: ' + errorMessage(e) });
      setSaving(false);
    }
  };

  const onTestConnection = async () => {
    setTesting(true);
    setHealth(null);
    try {
      const result = await runHealthCheck(PLUGIN_ID);
      setHealth(result);
    } catch (e) {
      setHealth({ status: 'error', message: errorMessage(e) });
    } finally {
      setTesting(false);
    }
  };

  return (
    <div className={s.form} data-testid={testIds.appConfig.container}>
      <FieldSet label="Meraki Dashboard API">
        <Field
          label="API key"
          description="A Meraki Dashboard API key. Stored encrypted by Grafana and only used server-side."
        >
          <SecretInput
            width={60}
            id="meraki-api-key"
            data-testid={testIds.appConfig.apiKey}
            value={form.apiKey}
            isConfigured={form.isApiKeySet}
            placeholder="Paste your Meraki API key"
            onChange={onChange('apiKey')}
            onReset={onResetApiKey}
          />
        </Field>

        <Field
          label="Base URL"
          description={`Optional regional override. Defaults to ${DEFAULT_MERAKI_BASE_URL}.`}
          className={s.marginTop}
        >
          <Input
            width={60}
            id="meraki-base-url"
            data-testid={testIds.appConfig.baseUrl}
            value={form.baseUrl}
            placeholder={DEFAULT_MERAKI_BASE_URL}
            onChange={onChange('baseUrl')}
          />
        </Field>

        <Field
          label="Shared fraction"
          description="Fraction of the per-org 10 rps Meraki rate limit this Grafana instance may use. Set to 1/N across N HA replicas. Leave blank for 1.0."
          invalid={sharedFractionInvalid}
          error={sharedFractionInvalid ? 'Enter a number between 0 and 1 (exclusive of 0).' : undefined}
          className={s.marginTop}
        >
          <Input
            width={30}
            id="meraki-shared-fraction"
            data-testid={testIds.appConfig.sharedFraction}
            value={form.sharedFraction}
            placeholder="1.0"
            onChange={onChange('sharedFraction')}
          />
        </Field>

        <div className={s.actions}>
          <Button
            type="button"
            disabled={isSubmitDisabled}
            onClick={doSave}
            data-testid={testIds.appConfig.submit}
          >
            {saving ? 'Saving…' : 'Save'}
          </Button>
          <Button
            type="button"
            variant="secondary"
            icon="heart-rate"
            disabled={!canTestConnection || testing}
            onClick={onTestConnection}
            data-testid={testIds.appConfig.testConnection}
          >
            {testing ? 'Testing…' : 'Test connection'}
          </Button>
        </div>
      </FieldSet>

      {saveState && (
        <Alert
          severity={saveState.ok ? 'success' : 'error'}
          title={saveState.ok ? 'Settings saved' : 'Save failed'}
          onRemove={saveState.ok ? undefined : () => setSaveState(null)}
        >
          {saveState.message}
        </Alert>
      )}

      {health && (
        <Alert
          severity={health.status === 'ok' ? 'success' : 'error'}
          title={health.status === 'ok' ? 'Connected to Meraki' : 'Connection failed'}
          data-testid={testIds.appConfig.connectionResult}
          onRemove={() => setHealth(null)}
        >
          {health.message}
        </Alert>
      )}
    </div>
  );
};

export default AppConfig;

function parseSharedFraction(raw: string): number | null {
  if (raw === '') {
    return null;
  }
  const n = Number(raw);
  if (!Number.isFinite(n) || n <= 0 || n > 1) {
    return null;
  }
  return n;
}

function errorMessage(e: unknown): string {
  if (e instanceof Error) {
    return e.message;
  }
  if (typeof e === 'object' && e && 'data' in e) {
    const d = (e as { data?: { message?: string } }).data;
    if (d?.message) {
      return d.message;
    }
  }
  return String(e);
}

async function updatePlugin(pluginId: string, data: Partial<PluginMeta<AppJsonData>>) {
  const response = getBackendSrv().fetch({
    url: `/api/plugins/${pluginId}/settings`,
    method: 'POST',
    data,
  });
  await lastValueFrom(response);
}

async function runHealthCheck(pluginId: string): Promise<HealthResult> {
  const response = getBackendSrv().fetch<{ status: string; message: string }>({
    url: `/api/plugins/${pluginId}/health`,
    method: 'GET',
  });
  const { data } = await lastValueFrom(response);
  const ok = (data?.status ?? '').toLowerCase() === 'ok';
  return {
    status: ok ? 'ok' : 'error',
    message: data?.message || (ok ? 'Reachable.' : 'Unknown error.'),
  };
}

const getStyles = (theme: GrafanaTheme2) => ({
  form: css`
    padding: ${theme.spacing(3)} 0;
    max-width: 720px;
  `,
  marginTop: css`
    margin-top: ${theme.spacing(3)};
  `,
  actions: css`
    margin-top: ${theme.spacing(3)};
    display: flex;
    gap: ${theme.spacing(2)};
  `,
});
