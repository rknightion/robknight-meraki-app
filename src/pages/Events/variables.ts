import { CustomVariable } from '@grafana/scenes';

/**
 * `$productType` — static product-type filter for the Events scene. The
 * Meraki `/networks/{id}/events` endpoint requires a productType when the
 * target network spans multiple families, so we do NOT include an "All"
 * sentinel here: there's no sensible default value the API would accept.
 *
 * The six families match `MerakiProductType` in `src/types.ts`.
 */
export function productTypeVariable(): CustomVariable {
  return new CustomVariable({
    name: 'productType',
    label: 'Product type',
    query: 'wireless,appliance,switch,camera,cellularGateway,systemsManager',
    value: 'wireless',
    text: 'wireless',
    includeAll: false,
    isMulti: false,
  });
}

/**
 * `$eventType` — free-form multi-select filter for the Meraki event-type
 * code (e.g. `association`, `dhcp_lease`, `vpn_connectivity_change`). We
 * don't hydrate the list from the API because the vocabulary is huge and
 * per-family — instead, users type the codes they care about and the
 * backend forwards them through `q.Metrics` → `includedEventTypes[]`.
 *
 * An empty default means "no filter" — the backend passes through whatever
 * the caller supplies (including an empty slice) so the initial view shows
 * every event type for the selected product.
 */
export function eventTypeVariable(): CustomVariable {
  return new CustomVariable({
    name: 'eventType',
    label: 'Event types',
    query: '',
    value: '',
    text: '',
    includeAll: false,
    isMulti: true,
  });
}
