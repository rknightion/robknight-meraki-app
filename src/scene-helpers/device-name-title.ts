import { DataFrameView, dataFrameFromJSON } from '@grafana/data';
import { getBackendSrv } from '@grafana/runtime';
import type { SceneAppPage } from '@grafana/scenes';
import { lastValueFrom } from 'rxjs';

import { PLUGIN_ID } from '../constants';
import { QueryKind } from '../datasource/types';
import type { MerakiProductType } from '../types';
import { readAppJsonData } from './app-jsondata';

/**
 * When the operator has set `labelMode='name'`, swap a device-detail
 * page's title from the raw serial (baked in at factory-construction
 * time) to the device's human-friendly name. Silent no-op when
 * `labelMode='serial'`, when the URL has no `var-org`, or when the
 * device record cannot be resolved — the serial remains visible so the
 * breadcrumb always carries a usable identifier.
 *
 * Intentionally async-after-construction: the detail factory must
 * return a SceneAppPage synchronously so drilldown routing isn't
 * blocked on a network call. Scenes re-renders reactively when we
 * `setState({ title })` a tick later.
 */
export function applyDeviceNameTitle(
  page: SceneAppPage,
  serial: string,
  productType: MerakiProductType
): void {
  if (readAppJsonData()?.labelMode !== 'name') {
    return;
  }
  const orgId = readOrgFromUrl();
  if (!orgId) {
    return;
  }
  void resolveDeviceName(orgId, serial, productType).then((name) => {
    if (name && name.length > 0) {
      page.setState({ title: name });
    }
  });
}

function readOrgFromUrl(): string | undefined {
  if (typeof window === 'undefined') {
    return undefined;
  }
  const raw = new URLSearchParams(window.location.search).get('var-org');
  return raw ?? undefined;
}

async function resolveDeviceName(
  orgId: string,
  serial: string,
  productType: MerakiProductType
): Promise<string | undefined> {
  try {
    const url = `/api/plugins/${PLUGIN_ID}/resources/query`;
    const now = Date.now();
    const payload = {
      range: { from: now - 60 * 60 * 1000, to: now },
      maxDataPoints: 100,
      intervalMs: 1000,
      queries: [
        { refId: 'A', kind: QueryKind.Devices, orgId, productTypes: [productType] },
      ],
    };
    const obs = getBackendSrv().fetch<{ frames: object[] }>({
      url,
      method: 'POST',
      data: payload,
    });
    const { data } = await lastValueFrom(obs);
    const frames = (data?.frames ?? []).map((f) => dataFrameFromJSON(f as never));
    for (const frame of frames) {
      const view = new DataFrameView<{ serial?: string; name?: string }>(frame);
      for (let i = 0; i < view.length; i++) {
        const row = view.get(i);
        if (row.serial === serial && row.name) {
          return row.name;
        }
      }
    }
    return undefined;
  } catch {
    return undefined;
  }
}
