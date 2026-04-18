import React, { useEffect, useState } from 'react';
import { lastValueFrom } from 'rxjs';
import { SceneFlexItem, SceneReactObject } from '@grafana/scenes';
import { getBackendSrv } from '@grafana/runtime';
import { Alert, LinkButton } from '@grafana/ui';
import { PLUGIN_ID, ROUTES } from '../constants';
import { prefixRoute } from '../utils/utils.routing';

type PingResponse = {
  message?: string;
  configured?: boolean;
};

/**
 * Renders a "Cisco Meraki app not configured" banner above every scene that
 * depends on an API key, with a single CTA to the in-app Configuration page.
 *
 * Why this shape:
 *  - Scene queries silently 412 when the plugin has no API key. Users see
 *    blank panels with no hint as to why — this banner tells them.
 *  - Link targets the in-app `configuration` route (not `/plugins/<id>`) so
 *    the user stays inside the app shell and returns to the page they came
 *    from after saving.
 *  - Any fetch failure (network, non-JSON, etc.) is swallowed: the health
 *    check on the plugin settings page surfaces real errors, and showing a
 *    scary banner on top of every page when the ping endpoint is
 *    unreachable would be more noise than signal.
 */
export function ConfigGuard() {
  // `undefined` = still checking; `true`/`false` after the ping resolves.
  const [configured, setConfigured] = useState<boolean | undefined>(undefined);

  useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const obs = getBackendSrv().fetch<PingResponse>({
          url: `/api/plugins/${PLUGIN_ID}/resources/ping`,
          method: 'GET',
          showErrorAlert: false,
        });
        const { data } = await lastValueFrom(obs);
        if (!cancelled) {
          setConfigured(Boolean(data?.configured));
        }
      } catch {
        // Swallow network / decode failures — treat as "configured" so we
        // don't nag the user when the diagnostic endpoint is flaky.
        if (!cancelled) {
          setConfigured(true);
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  if (configured !== false) {
    return null;
  }

  return (
    <Alert severity="warning" title="Cisco Meraki app not configured">
      <p>
        Paste a Meraki Dashboard API key to start pulling organization, network, and device data.
      </p>
      <LinkButton href={prefixRoute(ROUTES.Configuration)} variant="primary" icon="cog">
        Open configuration
      </LinkButton>
    </Alert>
  );
}

/**
 * Wrap {@link ConfigGuard} in a SceneFlexItem so scenes can drop it in as
 * the first child of their root FlexLayout without plumbing React objects
 * manually. When the plugin is configured, {@link ConfigGuard} returns
 * `null` and the flex item collapses to zero height — no reserved space.
 */
export function configGuardFlexItem(): SceneFlexItem {
  return new SceneFlexItem({
    body: new SceneReactObject({ component: ConfigGuard }),
  });
}
