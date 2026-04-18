import { CustomVariable } from '@grafana/scenes';

/**
 * $licenseState — static license-state filter for the Insights Licensing tab.
 *
 * Mirrors the severity-via-metrics contract used by the Alerts area
 * (`pkg/plugin/query/insights.go:handleLicensesList` reuses `q.Metrics[0]` as
 * the state filter until a dedicated field lands on `MerakiQuery`). A
 * `CustomVariable` is served without an HTTP roundtrip; Meraki's license
 * states are a stable vocabulary and don't need variable hydration.
 *
 * The `All : ,` leading entry in the query spec is the `includeAll` sentinel
 * — picking it sends an empty string, which the backend treats as "no
 * filter". Duplicating `allValue: ''` keeps the URL representation
 * deterministic even when the user clicks around.
 */
export function licenseStateVariable(): CustomVariable {
  return new CustomVariable({
    name: 'licenseState',
    label: 'State',
    // Label : value syntax. "All" maps to empty string so the backend skips the filter.
    query: 'All : ,active,expired,expiring,recentlyQueued,unused,unusedActive',
    value: '',
    text: 'All',
    includeAll: true,
    defaultToAll: true,
    allValue: '',
    isMulti: false,
  });
}
