import { lastValueFrom } from 'rxjs';
import {
  DataQueryRequest,
  DataQueryResponse,
  DataSourceApi,
  DataSourceInstanceSettings,
  MetricFindValue,
  dataFrameFromJSON,
  getDefaultTimeRange,
  ScopedVars,
} from '@grafana/data';
import { getBackendSrv, getTemplateSrv } from '@grafana/runtime';
import {
  MerakiDSOptions,
  MerakiMetricFindValue,
  MerakiQuery,
  QueryKind,
} from './types';

const APP_PLUGIN_ID = 'rknightion-meraki-app';

/**
 * MerakiDataSource is a thin frontend-only data source — it proxies every query and every
 * variable-hydration call to the Cisco Meraki app plugin's resource endpoints. The app plugin
 * owns the Meraki API client, rate limiter, and cache. This design keeps credentials
 * server-side and centralizes rate limiting in one place.
 */
export class MerakiDataSource extends DataSourceApi<MerakiQuery, MerakiDSOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<MerakiDSOptions>) {
    super(instanceSettings);
  }

  applyTemplateVariables(query: MerakiQuery, scopedVars: ScopedVars): MerakiQuery {
    const tpl = getTemplateSrv();
    const next: MerakiQuery = { ...query };
    if (next.orgId) {
      next.orgId = tpl.replace(next.orgId, scopedVars);
    }
    if (next.networkIds?.length) {
      next.networkIds = next.networkIds.flatMap((id) =>
        splitMulti(tpl.replace(id, scopedVars, 'csv'))
      );
    }
    if (next.serials?.length) {
      next.serials = next.serials.flatMap((s) => splitMulti(tpl.replace(s, scopedVars, 'csv')));
    }
    return next;
  }

  async query(request: DataQueryRequest<MerakiQuery>): Promise<DataQueryResponse> {
    const range = request.range ?? getDefaultTimeRange();
    const payload = {
      range: {
        from: range.from.valueOf(),
        to: range.to.valueOf(),
      },
      maxDataPoints: request.maxDataPoints ?? 1000,
      intervalMs: request.intervalMs ?? 1000,
      queries: request.targets
        .filter((q) => !q.hide)
        .map((q) => this.applyTemplateVariables(q, request.scopedVars)),
    };

    const response = await this.post<QueryResponseEnvelope>('query', payload);
    const frames = (response.frames ?? []).map((f) => dataFrameFromJSON(f));
    return { data: frames };
  }

  async metricFindQuery(query: MerakiQuery, options?: { scopedVars?: ScopedVars }): Promise<MetricFindValue[]> {
    const payload = {
      query: this.applyTemplateVariables(query, options?.scopedVars ?? {}),
    };
    const response = await this.post<{ values: MerakiMetricFindValue[] }>('metricFind', payload);
    return (response.values ?? []).map((v) => ({ text: v.text, value: v.value }));
  }

  async testDatasource(): Promise<{ status: 'success' | 'error'; message: string }> {
    try {
      const res = await getBackendSrv().get<{ status: string; message: string }>(
        `/api/plugins/${APP_PLUGIN_ID}/health`
      );
      const ok = (res?.status ?? '').toLowerCase() === 'ok';
      return {
        status: ok ? 'success' : 'error',
        message: res?.message ?? (ok ? 'Reachable.' : 'Unknown error.'),
      };
    } catch (e) {
      return { status: 'error', message: errorMessage(e) };
    }
  }

  /**
   * Convenience wrappers for variable selectors — scenes call these directly when they need a
   * specific list (orgs, networks, sensor metrics) instead of going via metricFindQuery.
   */
  async listOrganizations(): Promise<MerakiMetricFindValue[]> {
    return this.metricFindQuery({ refId: 'orgs', kind: QueryKind.Organizations });
  }

  async listNetworks(orgId: string): Promise<MerakiMetricFindValue[]> {
    return this.metricFindQuery({ refId: 'networks', kind: QueryKind.Networks, orgId });
  }

  private async post<T>(path: string, body: unknown): Promise<T> {
    const url = `/api/plugins/${APP_PLUGIN_ID}/resources/${path}`;
    const obs = getBackendSrv().fetch<T>({ url, method: 'POST', data: body });
    const { data } = await lastValueFrom(obs);
    return data;
  }
}

interface QueryResponseEnvelope {
  // Grafana's wire format for data frames (returned by dataFrameToJSON on the backend).
  frames: object[];
}

function splitMulti(raw: string): string[] {
  if (!raw) {
    return [];
  }
  return raw
    .split(',')
    .map((s) => s.trim())
    .filter((s) => s.length > 0);
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
