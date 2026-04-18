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
  Combobox,
  Field,
  FieldSet,
  Input,
  InlineSwitch,
  LinkButton,
  RadioButtonGroup,
  SecretInput,
  useStyles2,
} from '@grafana/ui';
import { testIds } from '../testIds';
import { AppJsonData, AppSecureJsonData, DeviceLabelMode } from '../../types';
import { DEFAULT_MERAKI_BASE_URL, MERAKI_REGIONS, PLUGIN_ID, ROUTES } from '../../constants';
import { prefixRoute } from '../../utils/utils.routing';

const CUSTOM_REGION_LABEL = 'Custom…';

const DEFAULT_LABEL_MODE: DeviceLabelMode = 'serial';

const LABEL_MODE_OPTIONS: Array<{ label: string; value: DeviceLabelMode; description?: string }> = [
  { label: 'Serial', value: 'serial', description: 'Use the raw Meraki device serial (e.g. Q3CC-HV6P-H5XK).' },
  { label: 'Name', value: 'name', description: 'Use the device name; falls back to serial when unset.' },
];

export type AppConfigProps = PluginConfigPageProps<AppPluginMeta<AppJsonData>>;

/**
 * Variants of the shared config form:
 *  - `catalog`: minimal first-time-setup flow — API key + base URL + Save +
 *    Test connection, nothing else. Rendered at `/plugins/<id>` so admins
 *    arriving from the plugin catalog can enter a key and validate
 *    reachability without being confronted with tuning knobs.
 *  - `full`: every setting — adds shared-fraction and device-label-mode
 *    fields. Rendered inside the app at `/a/<id>/configuration` for
 *    ongoing management.
 *
 * Both variants POST to the same `/api/plugins/<id>/settings` endpoint.
 * When the `catalog` variant saves, it preserves any existing
 * sharedFraction / labelMode the user has set elsewhere (we don't clear
 * fields we don't render).
 */
export type ConfigFormVariant = 'catalog' | 'full';

/**
 * Props for the pure-form component. Splitting this out lets the same form
 * render both at `/plugins/<id>` (wrapped in Grafana's PluginConfigPageProps
 * chrome) and at `/a/<id>/configuration` (mounted inside a scene via
 * SceneReactObject). Both entry points read the same plugin meta and POST
 * to the same `/api/plugins/<id>/settings` endpoint.
 */
export type MerakiConfigFormProps = {
  meta: AppPluginMeta<AppJsonData>;
  variant?: ConfigFormVariant;
};

type FormState = {
  /** Currently selected region label. `Custom…` means "trust whatever is in baseUrl". */
  region: string;
  baseUrl: string;
  sharedFraction: string;
  apiKey: string;
  isApiKeySet: boolean;
  labelMode: DeviceLabelMode;
  enableIPLimiter: boolean;
  showEmptyFamilies: boolean;
};

type HealthDetails = {
  email?: string;
  name?: string;
  twoFactorEnabled?: boolean;
  samlEnabled?: boolean;
  organizationCount?: number;
};

type HealthResult =
  | { status: 'ok' | 'error'; message: string; details?: HealthDetails }
  | null;
type SaveState = null | { ok: true; message: string } | { ok: false; message: string };

// Keep the "saved successfully, reloading…" banner on screen briefly so the user sees
// confirmation before the full-page reload swaps the DOM out from under them.
const RELOAD_DELAY_MS = 600;

/**
 * Thin wrapper Grafana calls via `addConfigPage` for `/plugins/<id>`. Renders
 * the catalog variant — just enough to get a first-time admin over the
 * "enter API key + validate reachability" line. Everything else lives in
 * the in-app Configuration page.
 */
const AppConfig = ({ plugin }: AppConfigProps) => (
  <MerakiConfigForm meta={plugin.meta} variant="catalog" />
);

export default AppConfig;

/**
 * Pure form body. Safe to mount anywhere that can supply an
 * `AppPluginMeta<AppJsonData>` — the `/plugins/<id>` config page, a scene
 * wrapper inside the app, a test harness, etc.
 */
export const MerakiConfigForm = ({ meta, variant = 'full' }: MerakiConfigFormProps) => {
  const s = useStyles2(getStyles);
  const { enabled, pinned, jsonData, secureJsonFields } = meta;

  // Prefer Grafana's canonical `secureJsonFields.merakiApiKey` — it's set by
  // the backend on every settings fetch and is the authoritative source for
  // "is the secret persisted". We only fall back to our bespoke
  // jsonData.isApiKeySet flag for first-save edge cases (and for backwards
  // compatibility with settings written by older builds of the plugin).
  const apiKeyPersisted = Boolean(secureJsonFields?.merakiApiKey) || Boolean(jsonData?.isApiKeySet);

  const [form, setForm] = useState<FormState>({
    region: regionForUrl(jsonData?.baseUrl ?? ''),
    baseUrl: jsonData?.baseUrl ?? '',
    sharedFraction:
      jsonData?.sharedFraction !== undefined ? String(jsonData.sharedFraction) : '',
    apiKey: '',
    isApiKeySet: apiKeyPersisted,
    labelMode: jsonData?.labelMode ?? DEFAULT_LABEL_MODE,
    enableIPLimiter: Boolean(jsonData?.enableIPLimiter),
    showEmptyFamilies: Boolean(jsonData?.showEmptyFamilies),
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
      // The catalog variant doesn't render shared-fraction or label-mode
      // fields, so its form state for those is whatever we initialised from
      // meta. We still write them back as-is on every save so we don't
      // silently clobber values the user changed in the in-app Configuration
      // page — full variant writes the current form values; catalog variant
      // writes through the persisted values from meta (not whatever default
      // the form happened to hold).
      const nextJsonData: AppJsonData = {
        baseUrl: form.baseUrl || undefined,
        sharedFraction:
          variant === 'catalog'
            ? jsonData?.sharedFraction
            : parsedSharedFraction ?? undefined,
        // Mark the key as set if the user is submitting a new value OR it was already set.
        isApiKeySet: form.isApiKeySet || Boolean(form.apiKey),
        labelMode:
          variant === 'catalog'
            ? jsonData?.labelMode
            : form.labelMode,
        enableIPLimiter:
          variant === 'catalog'
            ? jsonData?.enableIPLimiter
            : form.enableIPLimiter,
        showEmptyFamilies:
          variant === 'catalog'
            ? jsonData?.showEmptyFamilies
            : form.showEmptyFamilies,
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
          label="Region"
          description="Select a Meraki Dashboard region. Pick Custom… for sandbox / air-gapped deployments, then enter a base URL below."
          className={s.marginTop}
        >
          <Combobox<string>
            width={60}
            id="meraki-region"
            data-testid={testIds.appConfig.region}
            options={MERAKI_REGIONS.map((r) => ({ label: r.label, value: r.label }))}
            value={form.region}
            onChange={(opt) => {
              const nextLabel = opt?.value ?? CUSTOM_REGION_LABEL;
              const match = MERAKI_REGIONS.find((r) => r.label === nextLabel);
              // Custom keeps whatever the user has typed; known regions
              // overwrite baseUrl with the canonical URL so the form is
              // self-consistent on save.
              setForm((prev) => ({
                ...prev,
                region: nextLabel,
                baseUrl:
                  nextLabel === CUSTOM_REGION_LABEL
                    ? prev.baseUrl
                    : match?.url ?? prev.baseUrl,
              }));
            }}
          />
        </Field>

        <Field
          label="Base URL"
          description={`Optional regional override. Defaults to ${DEFAULT_MERAKI_BASE_URL}. Editable only when Region is set to Custom…`}
          className={s.marginTop}
        >
          <Input
            width={60}
            id="meraki-base-url"
            data-testid={testIds.appConfig.baseUrl}
            value={form.baseUrl}
            placeholder={DEFAULT_MERAKI_BASE_URL}
            disabled={form.region !== CUSTOM_REGION_LABEL}
            onChange={onChange('baseUrl')}
          />
        </Field>

        {variant === 'full' && (
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
        )}

        {variant === 'full' && (
          <Field
            label="Device label mode"
            description="How each per-device series is labeled on timeseries panels — applies across sensors, access points, appliances, cameras, and cellular gateways. Name mode resolves serials via the /devices feed (5 min cache) and falls back to the serial when a device has no name set."
            className={s.marginTop}
          >
            <RadioButtonGroup<DeviceLabelMode>
              id="meraki-label-mode"
              data-testid={testIds.appConfig.labelMode}
              options={LABEL_MODE_OPTIONS}
              value={form.labelMode}
              onChange={(value) =>
                setForm((prev) => ({ ...prev, labelMode: value ?? DEFAULT_LABEL_MODE }))
              }
            />
          </Field>
        )}

        {variant === 'full' && (
          <Field
            label="Enforce per-IP rate limit (100 rps)"
            description="Turn on for multi-tenant deployments where many org API keys egress via a single Grafana instance. Adds a secondary per-source-IP token bucket in front of the per-org bucket. Leave off for single-org setups — the per-org limiter is sufficient there."
            className={s.marginTop}
          >
            <InlineSwitch
              id="meraki-enable-ip-limiter"
              data-testid={testIds.appConfig.enableIPLimiter}
              value={form.enableIPLimiter}
              onChange={(event) => {
                const checked = (event.currentTarget as HTMLInputElement).checked;
                setForm((prev) => ({ ...prev, enableIPLimiter: checked }));
              }}
            />
          </Field>
        )}

        {variant === 'full' && (
          <Field
            label="Show empty device-family pages"
            description="By default, Appliances / Access Points / Switches / Cameras / Cellular Gateways / Sensors nav entries are hidden for the selected org when it has zero devices of that family. Turn on to always show every page (useful mid-deployment)."
            className={s.marginTop}
          >
            <InlineSwitch
              id="meraki-show-empty-families"
              data-testid={testIds.appConfig.showEmptyFamilies}
              value={form.showEmptyFamilies}
              onChange={(event) => {
                const checked = (event.currentTarget as HTMLInputElement).checked;
                setForm((prev) => ({ ...prev, showEmptyFamilies: checked }));
              }}
            />
          </Field>
        )}

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

      {variant === 'catalog' && form.isApiKeySet && (
        <div className={s.moreSettings}>
          <p className={s.moreSettingsLead}>
            Configuration moved into the app — shared rate-limit fraction, device label mode, and
            more live alongside your Home / Organizations / Sensors pages.
          </p>
          <LinkButton
            href={prefixRoute(ROUTES.Configuration)}
            icon="cog"
            variant="secondary"
          >
            Open app configuration
          </LinkButton>
        </div>
      )}

      {health && (
        <Alert
          severity={health.status === 'ok' ? 'success' : 'error'}
          title={health.status === 'ok' ? 'Connected to Meraki' : 'Connection failed'}
          data-testid={testIds.appConfig.connectionResult}
          onRemove={() => setHealth(null)}
        >
          {health.message}
          {health.status === 'ok' && health.details?.email && (
            <div className={s.connectionDetails}>
              <div>
                <strong>Signed in as:</strong>{' '}
                {health.details.name
                  ? `${health.details.name} (${health.details.email})`
                  : health.details.email}
              </div>
              {typeof health.details.organizationCount === 'number' && (
                <div>
                  <strong>Organizations visible:</strong>{' '}
                  {health.details.organizationCount}
                </div>
              )}
              {health.details.twoFactorEnabled && (
                <div>
                  <strong>Two-factor auth:</strong> enabled
                </div>
              )}
            </div>
          )}
        </Alert>
      )}
    </div>
  );
};

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
  // Grafana surfaces the CheckHealthResult.JSONDetails payload under
  // `data.details` on the /health response envelope. Older Grafana versions
  // that ignore JSONDetails just omit the field — the UI falls back to the
  // human-readable message in that case.
  const response = getBackendSrv().fetch<{
    status: string;
    message: string;
    details?: HealthDetails;
  }>({
    url: `/api/plugins/${pluginId}/health`,
    method: 'GET',
  });
  const { data } = await lastValueFrom(response);
  const ok = (data?.status ?? '').toLowerCase() === 'ok';
  return {
    status: ok ? 'ok' : 'error',
    message: data?.message || (ok ? 'Reachable.' : 'Unknown error.'),
    details: data?.details,
  };
}

/**
 * Map a saved baseUrl back to one of the MERAKI_REGIONS labels. Pure /
 * side-effect free so it's safe to call during render and in tests.
 *
 *  - Blank (`''` / undefined-coerced) ⇒ `Global / US` (the default region
 *    matches the default URL our backend uses when baseUrl is unset).
 *  - Exact URL match against a known region ⇒ that region's label.
 *  - Anything else ⇒ `Custom…`, which keeps the text input enabled.
 */
export function regionForUrl(baseUrl: string): string {
  if (!baseUrl) {
    return 'Global / US';
  }
  const match = MERAKI_REGIONS.find((r) => r.url && r.url === baseUrl);
  return match ? match.label : CUSTOM_REGION_LABEL;
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
  moreSettings: css`
    margin-top: ${theme.spacing(3)};
    padding: ${theme.spacing(2, 3)};
    border: 1px solid ${theme.colors.border.weak};
    border-radius: ${theme.shape.radius.default};
    background: ${theme.colors.background.secondary};
  `,
  moreSettingsLead: css`
    margin: 0 0 ${theme.spacing(2)} 0;
    color: ${theme.colors.text.secondary};
  `,
  connectionDetails: css`
    margin-top: ${theme.spacing(1.5)};
    display: flex;
    flex-direction: column;
    gap: ${theme.spacing(0.5)};
    font-size: ${theme.typography.bodySmall.fontSize};
  `,
});
