import { indexInstalled } from './indexInstalled';
import {
  BundleSavedConfig,
  GroupStateDto,
  InstalledRuleInfo,
  RuleGroupDef,
  RuleTemplateDef,
} from './types';

/**
 * Pure seeder used by the first-render init effect. Splitting this out of
 * the component keeps the effect's body free of setState sequences (lint:
 * `react-hooks/set-state-in-effect`) and makes the initialisation logic
 * unit-testable in isolation.
 *
 * Precedence for every field:
 *   1. If the user has a persisted saved value → use it.
 *   2. Else fall back to live status (installed → installed=true,
 *      enabled flag mirrored from the live row).
 *   3. Else fall back to template defaults.
 */
export function seedDesired<T extends RuleTemplateDef>(
  groups: Array<RuleGroupDef<T>>,
  installed: InstalledRuleInfo[],
  saved: BundleSavedConfig | undefined,
): {
  groups: Record<string, GroupStateDto>;
  thresholds: Record<string, Record<string, Record<string, unknown>>>;
  openGroups: Record<string, boolean>;
} {
  const savedGroups = saved?.groups ?? {};
  const savedThresholds = saved?.thresholds ?? {};
  const installedIndex = indexInstalled(installed);

  const nextGroups: Record<string, GroupStateDto> = {};
  const nextThresholds: Record<string, Record<string, Record<string, unknown>>> = {};
  const openGroups: Record<string, boolean> = {};

  for (const group of groups) {
    const savedGroup = savedGroups[group.id];
    const installedUnderGroup = installedIndex[group.id] ?? {};
    const hasAnyInstalled = Object.keys(installedUnderGroup).length > 0;

    const rulesEnabled: Record<string, boolean> = {};
    for (const tpl of group.templates) {
      if (savedGroup?.rulesEnabled && tpl.id in savedGroup.rulesEnabled) {
        rulesEnabled[tpl.id] = Boolean(savedGroup.rulesEnabled[tpl.id]);
      } else if (installedUnderGroup[tpl.id]) {
        rulesEnabled[tpl.id] = installedUnderGroup[tpl.id].enabled;
      } else {
        rulesEnabled[tpl.id] = true;
      }
    }

    nextGroups[group.id] = {
      installed: savedGroup?.installed ?? hasAnyInstalled,
      rulesEnabled,
    };
    openGroups[group.id] = nextGroups[group.id].installed;

    const groupThresholds: Record<string, Record<string, unknown>> = {};
    for (const tpl of group.templates) {
      const perTpl: Record<string, unknown> = {};
      const savedPerTpl = savedThresholds[group.id]?.[tpl.id] ?? {};
      for (const th of tpl.thresholds) {
        if (th.key in savedPerTpl) {
          perTpl[th.key] = savedPerTpl[th.key];
        } else if (th.default !== undefined) {
          perTpl[th.key] = th.default;
        }
      }
      if (Object.keys(perTpl).length > 0) {
        groupThresholds[tpl.id] = perTpl;
      }
    }
    if (Object.keys(groupThresholds).length > 0) {
      nextThresholds[group.id] = groupThresholds;
    }
  }

  return { groups: nextGroups, thresholds: nextThresholds, openGroups };
}
