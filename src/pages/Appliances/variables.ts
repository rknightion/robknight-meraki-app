import { QueryVariable } from '@grafana/scenes';
import { deviceVariable } from '../../scene-helpers/variables';

/**
 * $mx — single-select appliance picker hydrated from the Meraki Devices
 * metricFind handler with `productTypes=['appliance']`. Delegates to the
 * shared `deviceVariable()` factory so the Access Points / Switches /
 * Appliances pickers all share the same wire shape.
 *
 * Kept single-select because per-appliance endpoints (uplink status,
 * loss/latency samples) accept one serial at a time; multi-select would
 * force panels to fan out into N frames per series and break the legend
 * contract.
 */
export function mxVariable(): QueryVariable {
  return deviceVariable({
    name: 'mx',
    label: 'Appliance',
    productType: 'appliance',
  });
}
