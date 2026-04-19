import { TextBoxVariable } from '@grafana/scenes';
import { clientVariable } from '../../scene-helpers/variables';

/**
 * `$client` — free-form MAC search variable used by the Search tab and the
 * per-client drilldown. Shared factory in `scene-helpers/variables.ts` so the
 * shape stays consistent if other pages start needing a client picker.
 *
 * Empty string means "no client selected" — the search-tab handler treats
 * that as "show the empty-state notice" rather than firing a request that
 * would 400 on the backend.
 */
export function clientSearchVariable(): TextBoxVariable {
  return clientVariable({ name: 'client', label: 'MAC' });
}
