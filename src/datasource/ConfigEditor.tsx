import React from 'react';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { Alert, LinkButton } from '@grafana/ui';
import { MerakiDSOptions } from './types';

const APP_PLUGIN_ID = 'robknight-meraki-app';

export type ConfigEditorProps = DataSourcePluginOptionsEditorProps<MerakiDSOptions>;

export function ConfigEditor(_: ConfigEditorProps) {
  return (
    <div>
      <Alert
        severity="info"
        title="Configuration lives on the Cisco Meraki app plugin"
      >
        This data source has no fields of its own. The Meraki API key and region are configured
        once on the Cisco Meraki app plugin; every query from this data source is proxied through
        the app plugin&apos;s backend, which owns the rate limiter and cache.
      </Alert>
      <div style={{ marginTop: 16, display: 'flex', gap: 8 }}>
        <LinkButton href={`/plugins/${APP_PLUGIN_ID}`} icon="cog" variant="primary">
          Open Meraki app configuration
        </LinkButton>
      </div>
    </div>
  );
}
