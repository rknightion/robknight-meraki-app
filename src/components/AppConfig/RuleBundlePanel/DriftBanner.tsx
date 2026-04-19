import React from 'react';
import { Alert } from '@grafana/ui';

export type DriftBannerProps = {
  testId?: string;
  title?: string;
  body?: string;
};

export function DriftBanner({
  testId,
  title = 'External edits detected',
  body = 'One or more managed rules differ from the desired state configured here. Running reconcile will revert them.',
}: DriftBannerProps) {
  return (
    <Alert severity="info" title={title} data-testid={testId}>
      {body}
    </Alert>
  );
}
