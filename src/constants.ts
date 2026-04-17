import pluginJson from './plugin.json';

export const PLUGIN_ID = pluginJson.id;
export const PLUGIN_BASE_URL = `/a/${pluginJson.id}`;

export enum ROUTES {
  Home = 'home',
  Organizations = 'organizations',
  Sensors = 'sensors',
}

export const DEFAULT_MERAKI_BASE_URL = 'https://api.meraki.com/api/v1';
