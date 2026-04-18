import React, { useEffect, useState } from 'react';
import { AppPluginMeta } from '@grafana/data';
import { getBackendSrv } from '@grafana/runtime';
import { Alert, LoadingPlaceholder } from '@grafana/ui';
import { lastValueFrom } from 'rxjs';
import { MerakiConfigForm } from '../../components/AppConfig/AppConfig';
import { AppJsonData } from '../../types';
import { PLUGIN_ID } from '../../constants';

/**
 * ConfigurationPanel mounts the shared config form inside the app's scene
 * tree. We explicitly fetch `/api/plugins/<id>/settings` on mount instead
 * of reading `usePluginMeta()` — Grafana's AppRootProps.meta doesn't
 * reliably populate `jsonData.isApiKeySet` or `secureJsonFields` in the
 * in-app context, which previously left the Save button permanently
 * disabled (the form thought no API key was saved). The settings endpoint
 * is authoritative and returns the same PluginMeta shape the classic
 * `/plugins/<id>` config page sees.
 */
export function ConfigurationPanel() {
  const [meta, setMeta] = useState<AppPluginMeta<AppJsonData> | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const obs = getBackendSrv().fetch<AppPluginMeta<AppJsonData>>({
          url: `/api/plugins/${PLUGIN_ID}/settings`,
          method: 'GET',
        });
        const { data } = await lastValueFrom(obs);
        if (!cancelled) {
          setMeta(data);
        }
      } catch (e) {
        if (!cancelled) {
          setError(errorMessage(e));
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  if (error) {
    return (
      <Alert severity="error" title="Failed to load plugin settings">
        {error}
      </Alert>
    );
  }

  if (!meta) {
    return <LoadingPlaceholder text="Loading configuration…" />;
  }

  return <MerakiConfigForm meta={meta} />;
}

function errorMessage(e: unknown): string {
  if (e instanceof Error) {
    return e.message;
  }
  if (typeof e === 'object' && e && 'data' in e) {
    const d = (e as { data?: { message?: string } }).data;
    if (d?.message) {
      return d.message;
    }
  }
  return String(e);
}
