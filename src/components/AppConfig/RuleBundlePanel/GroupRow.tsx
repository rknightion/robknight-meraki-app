import React, { ReactNode } from 'react';
import { css } from '@emotion/css';
import { GrafanaTheme2 } from '@grafana/data';
import { Checkbox, Collapse, useStyles2 } from '@grafana/ui';
import { ThresholdInput } from './ThresholdInput';
import { GroupStateDto, RuleGroupDef, RuleTemplateDef } from './types';

export type GroupRowTestIds = {
  groupCard: (groupId: string) => string;
  groupInstallToggle: (groupId: string) => string;
  templateRow: (groupId: string, templateId: string) => string;
  ruleEnabled: (groupId: string, templateId: string) => string;
  thresholdInput: (groupId: string, templateId: string, key: string) => string;
};

export type GroupRowProps<T extends RuleTemplateDef> = {
  group: RuleGroupDef<T>;
  groupState: GroupStateDto;
  thresholds: Record<string, Record<string, unknown>>;
  isOpen: boolean;
  onToggleInstall: (next: boolean) => void;
  onToggleOpen: (next: boolean) => void;
  onToggleRuleEnabled: (templateId: string, next: boolean) => void;
  onThresholdChange: (templateId: string, key: string, value: unknown) => void;
  renderTemplateMeta?: (template: T) => ReactNode;
  emptyHint: string;
  testIds: GroupRowTestIds;
};

export function GroupRow<T extends RuleTemplateDef>({
  group,
  groupState,
  thresholds,
  isOpen,
  onToggleInstall,
  onToggleOpen,
  onToggleRuleEnabled,
  onThresholdChange,
  renderTemplateMeta,
  emptyHint,
  testIds,
}: GroupRowProps<T>) {
  const s = useStyles2(getStyles);
  return (
    <div className={s.groupCard} data-testid={testIds.groupCard(group.id)}>
      <div className={s.groupHeader}>
        <Checkbox
          label={`${group.displayName} (${group.templates.length} rule${group.templates.length === 1 ? '' : 's'})`}
          value={groupState.installed}
          onChange={(e) => onToggleInstall((e.currentTarget as HTMLInputElement).checked)}
          data-testid={testIds.groupInstallToggle(group.id)}
        />
      </div>
      <Collapse
        label={<span className={s.groupMeta}>Rule detail</span>}
        isOpen={isOpen}
        onToggle={onToggleOpen}
      >
        <div className={s.groupBody}>
          {groupState.installed ? (
            <div className={s.templateList}>
              {group.templates.map((tpl) => (
                <TemplateRow
                  key={tpl.id}
                  groupId={group.id}
                  template={tpl}
                  enabled={groupState.rulesEnabled[tpl.id] ?? true}
                  thresholds={thresholds[tpl.id] ?? {}}
                  onEnabledChange={(next) => onToggleRuleEnabled(tpl.id, next)}
                  onThresholdChange={(key, value) => onThresholdChange(tpl.id, key, value)}
                  renderMeta={renderTemplateMeta}
                  testIds={testIds}
                />
              ))}
            </div>
          ) : (
            <p className={s.groupHint}>{emptyHint}</p>
          )}
        </div>
      </Collapse>
    </div>
  );
}

type TemplateRowProps<T extends RuleTemplateDef> = {
  groupId: string;
  template: T;
  enabled: boolean;
  thresholds: Record<string, unknown>;
  onEnabledChange: (next: boolean) => void;
  onThresholdChange: (key: string, value: unknown) => void;
  renderMeta?: (template: T) => ReactNode;
  testIds: GroupRowTestIds;
};

function TemplateRow<T extends RuleTemplateDef>({
  groupId,
  template,
  enabled,
  thresholds,
  onEnabledChange,
  onThresholdChange,
  renderMeta,
  testIds,
}: TemplateRowProps<T>) {
  const s = useStyles2(getStyles);
  return (
    <div className={s.templateRow} data-testid={testIds.templateRow(groupId, template.id)}>
      <div className={s.templateHeader}>
        <Checkbox
          value={enabled}
          onChange={(e) => onEnabledChange((e.currentTarget as HTMLInputElement).checked)}
          label={template.displayName}
          data-testid={testIds.ruleEnabled(groupId, template.id)}
        />
        {renderMeta?.(template)}
      </div>

      {template.thresholds.length > 0 && (
        <div className={s.thresholdGrid}>
          {template.thresholds.map((schema) => (
            <ThresholdInput
              key={schema.key}
              schema={schema}
              testId={testIds.thresholdInput(groupId, template.id, schema.key)}
              value={thresholds[schema.key]}
              onChange={(next) => onThresholdChange(schema.key, next)}
            />
          ))}
        </div>
      )}
    </div>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  groupBody: css`
    padding: ${theme.spacing(1, 2, 2, 2)};
  `,
  groupMeta: css`
    color: ${theme.colors.text.secondary};
    font-weight: normal;
  `,
  groupCard: css`
    margin-bottom: ${theme.spacing(2)};
    border: 1px solid ${theme.colors.border.weak};
    border-radius: ${theme.shape.radius.default};
    background: ${theme.colors.background.primary};
  `,
  groupHeader: css`
    padding: ${theme.spacing(1.5, 2)};
    border-bottom: 1px solid ${theme.colors.border.weak};
  `,
  groupHint: css`
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
    margin: 0;
  `,
  templateList: css`
    display: flex;
    flex-direction: column;
    gap: ${theme.spacing(2)};
  `,
  templateRow: css`
    padding: ${theme.spacing(1.5)};
    border: 1px solid ${theme.colors.border.weak};
    border-radius: ${theme.shape.radius.default};
    background: ${theme.colors.background.secondary};
  `,
  templateHeader: css`
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: ${theme.spacing(2)};
    margin-bottom: ${theme.spacing(1)};
  `,
  thresholdGrid: css`
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
    gap: ${theme.spacing(1, 2)};
  `,
});
