import { useCallback, useEffect, useState } from 'react';
import { lastValueFrom } from 'rxjs';
import { getBackendSrv } from '@grafana/runtime';
import { PLUGIN_ID } from '../../constants';
import { AlertsTemplatesResponse } from './alertsTypes';

export type UseAlertsTemplatesResult = {
  data: AlertsTemplatesResponse | null;
  loading: boolean;
  error: string | null;
  refetch: () => void;
};

/**
 * Fetches `/resources/alerts/templates` — the static registry of bundled
 * rule groups + templates + threshold schemas. The response is immutable
 * per plugin build, so a single fetch on mount is sufficient; `refetch` is
 * exposed mainly so tests (and a future "reload templates" button) can
 * force a re-read without remounting.
 */
export function useAlertsTemplates(): UseAlertsTemplatesResult {
  const [data, setData] = useState<AlertsTemplatesResponse | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);

  const refetch = useCallback(() => setTick((t) => t + 1), []);

  useEffect(() => {
    // Wrapping the initial state flips inside the async body keeps all
    // setState calls off the synchronous effect path (react-hooks lint
    // rule: `set-state-in-effect`). A microtask delay is fine — the
    // initial `loading=true` default state covers the first paint.
    let cancelled = false;
    (async () => {
      setLoading(true);
      setError(null);
      try {
        const obs = getBackendSrv().fetch<AlertsTemplatesResponse>({
          url: `/api/plugins/${PLUGIN_ID}/resources/alerts/templates`,
          method: 'GET',
          showErrorAlert: false,
        });
        const res = await lastValueFrom(obs);
        if (!cancelled) {
          setData(res.data);
          setLoading(false);
        }
      } catch (e) {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : String(e));
          setLoading(false);
        }
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [tick]);

  return { data, loading, error, refetch };
}
