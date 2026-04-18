import React from 'react';
import { css } from '@emotion/css';
import { GrafanaTheme2 } from '@grafana/data';
import { useStyles2 } from '@grafana/ui';
import { testIds } from '../../components/testIds';

/**
 * §4.4.5 — Home reworked.
 *
 * The previous welcome block + CTA grid was redundant with the left
 * sidebar nav (Organisations / Access Points / Switches / Sensors etc.
 * all live there). The Home page now lands operators straight on the
 * KPI row, so this component collapses to a single-line hint banner
 * (~40 px) that just tells users what they're looking at.
 *
 * If the API key isn't configured `<ConfigGuard/>` (still Row 1 of the
 * scene) surfaces the prominent banner + "Configure API key" CTA, so
 * this intro no longer needs to carry that affordance.
 */
export function HomeIntro() {
  const styles = useStyles2(getStyles);

  return (
    <div className={styles.wrap} data-testid={testIds.home.container}>
      <span className={styles.title}>Cisco Meraki</span>
      <span className={styles.hint}>
        Org overview. Pick an organization above; drill into a family from the left nav.
      </span>
    </div>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  wrap: css`
    padding: ${theme.spacing(1, 2)};
    display: flex;
    align-items: center;
    gap: ${theme.spacing(2)};
    height: 40px;
    border-bottom: 1px solid ${theme.colors.border.weak};
  `,
  title: css`
    font-weight: ${theme.typography.fontWeightMedium};
    color: ${theme.colors.text.primary};
  `,
  hint: css`
    color: ${theme.colors.text.secondary};
  `,
});
