import { CustomVariable } from '@grafana/scenes';

/**
 * $severity — static severity filter for the Alerts scene and the per-org
 * Alerts tab. `CustomVariable` is served without an HTTP roundtrip; the three
 * Meraki severity buckets (`info`, `warning`, `critical`) are a stable
 * vocabulary and don't need to be hydrated from the API.
 *
 * The Go backend reuses `MerakiQuery.Metrics` as the severity filter for
 * Alerts queries (a decision from the B2 agent to avoid adding a dedicated
 * `severity` field and racing other in-flight queries). An empty string means
 * "no filter" — the backend treats the `''` value as "match all". That's why
 * `allValue: ''` is the contract: when the user picks `All`, we send an empty
 * string and the backend skips the filter entirely.
 */
export function severityVariable(): CustomVariable {
  return new CustomVariable({
    name: 'severity',
    label: 'Severity',
    // CustomVariable uses a pipe- or comma-delimited spec. Each entry is
    // `label : value`; when the label is omitted the value is reused for the
    // display name. The leading `All : ,` entry is the includeAll sentinel —
    // but `CustomVariable` also honours `includeAll`, so we duplicate here for
    // clarity / URL determinism.
    query: 'info,warning,critical',
    value: '',
    text: 'All',
    includeAll: true,
    defaultToAll: true,
    allValue: '',
    isMulti: false,
  });
}
