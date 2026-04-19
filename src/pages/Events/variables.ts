import { CustomVariable } from '@grafana/scenes';

/**
 * `$productType` — product-type filter for the Events scene. The Meraki
 * `/networks/{id}/events` endpoint requires a productType when the target
 * network spans multiple families, so the "All" sentinel here (allValue:
 * '') is expanded server-side: when the backend sees an empty productType
 * it fans out one events call per family that the network actually has,
 * merges the results, and returns a single frame. That keeps the cost
 * bounded (N families per network, sequential under the rate-limiter)
 * while giving operators a single "show me everything" option.
 *
 * The seven families match Meraki's full product-type vocabulary
 * (superset of `MerakiProductType` in `src/types.ts` — events scenes also
 * cover Systems Manager, which has its own event codes).
 */
export function productTypeVariable(): CustomVariable {
  return new CustomVariable({
    name: 'productType',
    label: 'Product type',
    query: 'wireless,appliance,switch,camera,cellularGateway,sensor,systemsManager',
    value: '',
    text: 'All',
    includeAll: true,
    allValue: '',
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
