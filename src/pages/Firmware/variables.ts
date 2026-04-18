import { CustomVariable } from '@grafana/scenes';

/**
 * `$eoxStatus` — single-select EOX-bucket filter for the EOL device table.
 *
 * Why a CustomVariable rather than a QueryVariable: the available bucket
 * values are documented Meraki enums (endOfSale / endOfSupport /
 * nearEndOfSupport); there is nothing to hydrate from the API. A
 * `CustomVariable` keeps the choices baked into the scene and means the
 * dropdown is populated even before the operator picks an org.
 *
 * `includeAll: true` with `allValue: ''` matches the rest of the app's
 * variable convention — the empty string is forwarded to the backend as
 * `q.metrics[0]`, and the handler treats an empty list as "all three
 * buckets" (handleDeviceEol in pkg/plugin/query/firmware.go).
 */
export function eoxStatusVariable(): CustomVariable {
  return new CustomVariable({
    name: 'eoxStatus',
    label: 'EOX status',
    // Comma-separated label : value pairs per Grafana's CustomVariable syntax.
    query:
      'All EOX devices : ,' +
      'Near end-of-support : nearEndOfSupport,' +
      'End-of-support : endOfSupport,' +
      'End-of-sale : endOfSale',
    value: '',
    text: 'All EOX devices',
    includeAll: false,
    isMulti: false,
  });
}
