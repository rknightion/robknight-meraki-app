import { DataSourceRef } from '@grafana/schema';

/**
 * Stable UID for the nested Meraki data source. Matches the provisioning/datasources/meraki.yaml
 * entry so scenes and dashboards can reference the data source without a lookup.
 */
export const MERAKI_DS_UID = 'rknightion-meraki-ds';
export const MERAKI_DS_TYPE = 'rknightion-meraki-datasource';

export const MERAKI_DS_REF: DataSourceRef = {
  type: MERAKI_DS_TYPE,
  uid: MERAKI_DS_UID,
};
