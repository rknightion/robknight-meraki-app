/**
 * Device label mode — controls how each per-device series is labeled on
 * timeseries panels across every Meraki device family the plugin supports
 * (sensors, access points, switches, appliances, cameras, cellular gateways).
 *  - `serial` (default): raw Meraki device serial, e.g. `Q3CC-HV6P-H5XK`.
 *  - `name`: the human-friendly device name from `/organizations/{id}/devices`
 *    (falls back to the serial when the device has no name set).
 */
export type DeviceLabelMode = 'serial' | 'name';

/**
 * App-level non-secret configuration stored in Grafana plugin settings.
 */
export interface AppJsonData {
  baseUrl?: string;
  sharedFraction?: number;
  isApiKeySet?: boolean;
  labelMode?: DeviceLabelMode;
  /**
   * Opt-in per-source-IP rate limiter. When true, the Go backend adds a
   * secondary 100 rps / 200 burst token bucket keyed on "ip" in front of the
   * per-org bucket. Useful only for multi-tenant deployments where many org
   * keys egress through a single Grafana instance; leave off for the common
   * single-org / single-replica case.
   */
  enableIPLimiter?: boolean;
  /**
   * Opt-in spine cache warmer. When true, the Go backend launches a
   * background goroutine that periodically refreshes the `organizations`
   * and per-org `organizationNetworks` cache entries so first-paint on a
   * cold page never pays the Meraki round-trip. Default off — small
   * single-org installs see little benefit. Only safe to enable when the
   * plugin instance is running with a real Meraki API key (no-op when
   * unconfigured).
   */
  prefetchSpine?: boolean;
  /**
   * When true, the app shows every device-family nav page even if the
   * selected org has zero devices of that family. Default (undefined/false)
   * hides Appliances / Access Points / Switches / Cameras / Cellular
   * Gateways / Sensors nav entries with no underlying devices so an org
   * that only runs MR/MS/MT isn't cluttered with empty pages.
   */
  showEmptyFamilies?: boolean;
  /**
   * Bundled alert-rules install state. Mirrors the Go-side
   * `pkg/plugin.appAlertsConfig` shape. Populated by the AppConfig UI and
   * by the `/alerts/reconcile` resource endpoint. Persistence note: the
   * runtime `lastReconciledAt` + `lastReconcileSummary` fields are ALSO
   * written to a plugin-local JSON file by the backend so /alerts/status
   * can answer after a plugin restart without waiting for Grafana to push
   * fresh jsonData — the jsonData mirror here is the authoritative copy
   * once the frontend saves settings.
   */
  alerts?: AlertsConfig;
  /**
   * Bundled Grafana-managed recording-rules install state. Mirrors the
   * Go-side `pkg/plugin.appRecordingsConfig`. Populated by the AppConfig
   * UI's RecordingsPanel and by `/recordings/reconcile`. Unlike alerts,
   * this config carries the operator-picked `targetDatasourceUid` — the
   * reconciler refuses to write rules without a target (412). Runtime
   * reconcile telemetry is also dual-persisted to a plugin-local JSON
   * file so `/recordings/status` can answer after a restart.
   */
  recordings?: RecordingsConfig;
}

/**
 * Per-group install state: whether the group is globally installed +
 * per-template enabled flags. A group with `installed=false` means the
 * reconciler will DELETE every rule under it regardless of `rulesEnabled`.
 * Mirrors `pkg/plugin.appAlertsGroupState`.
 */
export interface AlertsGroupState {
  installed: boolean;
  rulesEnabled: Record<string, boolean>;
}

/**
 * Summary counters from the most recent reconcile run. Four numbers (no
 * UIDs) — the detailed per-rule outcome lives in the synchronous
 * `ReconcileResult` returned from POST /alerts/reconcile, not here.
 */
export interface AlertsReconcileSummary {
  created: number;
  updated: number;
  deleted: number;
  failed: number;
}

/**
 * App-wide bundled alerts configuration. Mirrors the Go-side
 * `pkg/plugin.appAlertsConfig` shape. Every field is optional so a fresh
 * install serialises as `{}` rather than a partially-populated object.
 */
export interface AlertsConfig {
  /**
   * group-id → group state. Absent entries are treated as `installed=false`.
   */
  groups?: Record<string, AlertsGroupState>;
  /**
   * Threshold overrides, indexed `[groupId][templateId][thresholdKey]`.
   * The innermost value type is `unknown` because thresholds are a union
   * of string / number / boolean / string[] depending on the template's
   * schema — the UI layer validates against the Go-provided schema in
   * `/alerts/templates` rather than at the type boundary.
   */
  thresholds?: Record<string, Record<string, Record<string, unknown>>>;
  /**
   * ISO-8601 timestamp of the most recent reconcile. Absent on fresh
   * installs.
   */
  lastReconciledAt?: string;
  lastReconcileSummary?: AlertsReconcileSummary;
}

/**
 * Per-group install state for the recording-rules bundle. Same shape as
 * `AlertsGroupState` — kept as a distinct type so a future divergence
 * (e.g. per-group evaluation interval override) doesn't force reshaping
 * on the alerts side.
 */
export interface RecordingsGroupState {
  installed: boolean;
  rulesEnabled: Record<string, boolean>;
}

/**
 * Summary counters from the most recent recording-rules reconcile.
 * Four numbers; per-rule outcomes live in the synchronous
 * `ReconcileResult` returned from POST /recordings/reconcile.
 */
export interface RecordingsReconcileSummary {
  created: number;
  updated: number;
  deleted: number;
  failed: number;
}

/**
 * App-wide bundled recording-rules configuration. Mirrors the Go-side
 * `pkg/plugin.appRecordingsConfig` shape. Every field is optional so a
 * fresh install serialises as `{}` rather than a partially-populated
 * object.
 */
export interface RecordingsConfig {
  /**
   * UID of the Prometheus-compatible data source every recording rule
   * writes samples into. The DataSourcePicker in RecordingsPanel
   * populates this; `/recordings/reconcile` returns 412 Precondition
   * Failed when empty.
   */
  targetDatasourceUid?: string;
  /**
   * group-id → group state. Absent entries are treated as `installed=false`.
   */
  groups?: Record<string, RecordingsGroupState>;
  /**
   * Threshold overrides, indexed `[groupId][templateId][thresholdKey]`.
   * Same permissive shape as `AlertsConfig.thresholds`.
   */
  thresholds?: Record<string, Record<string, Record<string, unknown>>>;
  /**
   * ISO-8601 timestamp of the most recent reconcile. Absent on fresh
   * installs.
   */
  lastReconciledAt?: string;
  lastReconcileSummary?: RecordingsReconcileSummary;
}

/**
 * App-level secret configuration. Stored encrypted by Grafana; only ever read server-side.
 */
export interface AppSecureJsonData {
  merakiApiKey?: string;
}

export type MerakiProductType =
  | 'wireless'
  | 'switch'
  | 'appliance'
  | 'sensor'
  | 'camera'
  | 'cellularGateway';

export type SensorMetric =
  | 'temperature'
  | 'humidity'
  | 'door'
  | 'water'
  | 'co2'
  | 'pm25'
  | 'tvoc'
  | 'noise'
  | 'battery'
  | 'indoorAirQuality';
