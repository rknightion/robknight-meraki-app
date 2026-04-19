import { useCallback, useEffect, useState } from 'react';
import { lastValueFrom } from 'rxjs';
import { getBackendSrv } from '@grafana/runtime';
import { PLUGIN_ID } from '../../constants';
import { RecordingsStatusResponse } from './recordingsTypes';

export type UseRecordingsStatusResult = {
  data: RecordingsStatusResponse | null;
  loading: boolean;
  error: string | null;
  refetch: () => void;
};

/**
 * Fetches `/resources/recordings/status` — the live set of currently-installed
 * managed recording rules + last reconcile telemetry + the echo of the
 * operator-picked target datasource UID. Called on mount and whenever
 * `refetch()` is invoked (e.g. after a successful reconcile).
 */
export function useRecordingsStatus(): UseRecordingsStatusResult {
  const [data, setData] = useState<RecordingsStatusResponse | null>(null);
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
        const obs = getBackendSrv().fetch<RecordingsStatusResponse>({
          url: `/api/plugins/${PLUGIN_ID}/resources/recordings/status`,
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
