import { QueryVariable } from '@grafana/scenes';
import { deviceVariable } from '../../scene-helpers/variables';

/**
 * `$mg` — single-select cellular-gateway picker hydrated from the Meraki
 * Devices metricFind handler filtered to `productTypes=['cellularGateway']`.
 * Backed by the shared {@link deviceVariable} factory so the cascade and
 * single-select semantics match the MR/MS/MX/MV pickers.
 */
export function mgVariable(): QueryVariable {
  return deviceVariable({ name: 'mg', label: 'Gateway', productType: 'cellularGateway' });
}
