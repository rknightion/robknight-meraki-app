import { BundleDesiredState, InstalledRuleInfo } from './types';

export function detectDrift(
  status: { installed: InstalledRuleInfo[] } | null,
  desired: BundleDesiredState,
): boolean {
  if (!status) {
    return false;
  }
  for (const row of status.installed) {
    const groupState = desired.groups[row.groupId];
    if (!groupState) {
      // Rule lives in Grafana but the user hasn't loaded/chosen the group
      // yet — ignore rather than render a scary banner on first render.
      continue;
    }
    const wantEnabled = groupState.rulesEnabled[row.templateId] ?? true;
    const wantInstalled = groupState.installed;
    if (!wantInstalled) {
      // User wants this group uninstalled but rules still live — drift.
      return true;
    }
    if (wantEnabled !== row.enabled) {
      return true;
    }
  }
  return false;
}
