import { useCallback, useEffect, useState } from 'react';
import { lastValueFrom } from 'rxjs';
import { getBackendSrv } from '@grafana/runtime';
import { PLUGIN_ID } from '../../constants';
import { AlertsStatusResponse } from './alertsTypes';

export type UseAlertsStatusResult = {
  data: AlertsStatusResponse | null;
  loading: boolean;
  error: string | null;
  refetch: () => void;
};

/**
 * Fetches `/resources/alerts/status` — the live set of currently-installed
 * managed rules + last reconcile telemetry. Called on mount and whenever
 * `refetch()` is invoked (e.g. after a successful reconcile).
 */
export function useAlertsStatus(): UseAlertsStatusResult {
  const [data, setData] = useState<AlertsStatusResponse | null>(null);
  const [loading, setLoading] = useState<boolean>(true);
  const [error, setError] = useState<string | null>(null);
  const [tick, setTick] = useState(0);

  const refetch = useCallback(() => setTick((t) => t + 1), []);

  useEffect(() => {
    // Wrapping the initial state flips inside the async body keeps all
    // setState calls off the synchronous effect path (react-hooks lint
    // rule: `set-state-in-effect`).
    let cancelled = false;
    (async () => {
      setLoading(true);
      setError(null);
      try {
        const obs = getBackendSrv().fetch<AlertsStatusResponse>({
          url: `/api/plugins/${PLUGIN_ID}/resources/alerts/status`,
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
