import { InstalledRuleInfo } from './types';

export function indexInstalled(
  installed: InstalledRuleInfo[],
): Record<string, Record<string, InstalledRuleInfo>> {
  const out: Record<string, Record<string, InstalledRuleInfo>> = {};
  for (const row of installed) {
    if (!row.groupId || !row.templateId) {
      continue;
    }
    const bucket = (out[row.groupId] = out[row.groupId] ?? {});
    // When a group+template is installed across multiple orgs we only
    // retain one row for the "is this installed?" decision; enabled state
    // collapses to the last-seen row, which matches how the UI presents a
    // single enable toggle per template.
    bucket[row.templateId] = row;
  }
  return out;
}
