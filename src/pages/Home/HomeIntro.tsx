import React from 'react';
import { css } from '@emotion/css';
import { GrafanaTheme2 } from '@grafana/data';
import { LinkButton, useStyles2 } from '@grafana/ui';
import { ROUTES } from '../../constants';
import { prefixRoute } from '../../utils/utils.routing';
import { testIds } from '../../components/testIds';

export function HomeIntro() {
  const styles = useStyles2(getStyles);

  return (
    <div className={styles.wrap} data-testid={testIds.home.container}>
      <h2>Cisco Meraki</h2>
      <p className={styles.lead}>
        Observability for your Meraki organizations, networks, and devices — powered by the Meraki Dashboard API.
      </p>
      <p>
        This plugin queries <code>api.meraki.com</code> directly. Configure your Meraki API key on the
        configuration page to get started. Once configured, navigate to Organizations or Sensors to see your
        estate.
      </p>
      <div className={styles.actions}>
        <LinkButton href={prefixRoute(ROUTES.Configuration)} icon="cog" variant="primary">
          Configure API key
        </LinkButton>
        <LinkButton
          href="https://developer.cisco.com/meraki/api-v1/authorization/"
          icon="external-link-alt"
          variant="secondary"
          target="_blank"
        >
          How to get a Meraki API key
        </LinkButton>
      </div>
    </div>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  wrap: css`
    padding: ${theme.spacing(3)};
    max-width: 720px;
  `,
  lead: css`
    font-size: ${theme.typography.h5.fontSize};
    color: ${theme.colors.text.secondary};
  `,
  actions: css`
    margin-top: ${theme.spacing(3)};
    display: flex;
    gap: ${theme.spacing(2)};
    flex-wrap: wrap;
  `,
});
