// Re-export the shared Appliance link helpers from `src/scene-helpers/links.ts`
// so consumers inside the Appliances area can `import { appliancesUrl,
// applianceDetailUrl } from './links'` in the same shape as other areas (AP /
// Switches / Sensors each re-export their own links here). Keeping a per-area
// file leaves room to add area-specific helpers later (e.g. a deep-link into
// the Firewall tab) without moving every consumer back onto the shared file.
export { appliancesUrl, applianceDetailUrl } from '../../scene-helpers/links';
