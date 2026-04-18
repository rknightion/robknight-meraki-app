import { useEffect, useState } from 'react';
import { getBackendSrv } from '@grafana/runtime';
import { PLUGIN_ID } from '../constants';
import { QueryKind } from '../datasource/types';
import type { MerakiProductType } from '../types';

/**
 * Result of a one-shot `KindOrgProductTypes` call. Keys map 1:1 to Meraki
 * productType strings; values are int64 counts. A value > 0 means that
 * family is present in the org and its nav page should be shown.
 */
export type OrgDeviceFamilies = Partial<Record<MerakiProductType | 'systemsManager', number>>;

interface QueryResponseFrame {
  schema?: { fields?: Array<{ name: string; type: string }> };
  data?: { values?: unknown[][] };
}

interface QueryResponseBody {
  frames?: unknown[];
}

/**
 * Imperative one-shot loader — posts a single `KindOrgProductTypes` query to
 * the app's /resources/query endpoint and decodes the resulting wide frame
 * into a families map. Used by the hook below but also exported so call
 * sites outside React (e.g. tests) can grab the same data.
 */
export async function loadOrgDeviceFamilies(orgId: string | null | undefined): Promise<OrgDeviceFamilies> {
  if (!orgId) {
    return {};
  }
  try {
    const body = (await getBackendSrv().post(`/api/plugins/${PLUGIN_ID}/resources/query`, {
      queries: [{ refId: 'A', kind: QueryKind.OrgProductTypes, orgId }],
    })) as QueryResponseBody;
    const rawFrames = Array.isArray(body?.frames) ? body.frames : [];
    if (rawFrames.length === 0) {
      return {};
    }
    // Frames may arrive as pre-decoded JSON objects OR stringified blobs —
    // the backend uses `data.FrameToJSON` which yields a JSON string when
    // forwarded. Handle both shapes defensively.
    const first = rawFrames[0];
    const frame = (typeof first === 'string' ? JSON.parse(first) : first) as QueryResponseFrame;
    const fields = frame?.schema?.fields ?? [];
    const values = frame?.data?.values ?? [];
    const out: OrgDeviceFamilies = {};
    for (let i = 0; i < fields.length; i++) {
      const f = fields[i];
      const col = values[i];
      if (!f?.name || !Array.isArray(col) || col.length === 0) {
        continue;
      }
      const v = col[0];
      const n = typeof v === 'number' ? v : Number(v ?? 0);
      out[f.name as MerakiProductType] = Number.isFinite(n) ? n : 0;
    }
    return out;
  } catch (err) {
    // Swallow errors — the app falls back to "show every page" when the
    // hook returns an empty map, so a transient network or permission
    // failure doesn't black out the nav.
    console.warn('[meraki-app] loadOrgDeviceFamilies failed', err);
    return {};
  }
}

/**
 * React hook variant — refetches whenever `orgId` changes. `loading=true`
 * until the first response lands; during that window the caller should
 * treat every family as "maybe present" so the nav doesn't flicker.
 */
export function useOrgDeviceFamilies(orgId: string | null | undefined): {
  families: OrgDeviceFamilies;
  loading: boolean;
} {
  // Result state keyed to the orgId it was loaded for. Initial state differs
  // by orgId so the first render reports the right `loading` value without
  // ever calling setState inside the effect body.
  const [result, setResult] = useState<{ families: OrgDeviceFamilies; forOrg: string | null }>({
    families: {},
    forOrg: null,
  });

  useEffect(() => {
    if (!orgId) {
      return;
    }
    let cancelled = false;
    loadOrgDeviceFamilies(orgId).then((families) => {
      if (!cancelled) {
        setResult({ families, forOrg: orgId });
      }
    });
    return () => {
      cancelled = true;
    };
  }, [orgId]);

  const loading = Boolean(orgId) && result.forOrg !== orgId;
  const families = loading ? ({} as OrgDeviceFamilies) : result.families;
  return { families, loading };
}

/**
 * Family → productType key mapping used by the nav gate. Kept here (not in
 * constants.ts) so the backend field-name contract stays adjacent to its
 * consumers.
 */
export const FAMILY_KEYS = {
  appliance: 'appliance',
  wireless: 'wireless',
  switch: 'switch',
  camera: 'camera',
  cellularGateway: 'cellularGateway',
  sensor: 'sensor',
} as const;
