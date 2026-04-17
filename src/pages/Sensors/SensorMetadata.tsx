import React, { useEffect, useState } from 'react';
import { css } from '@emotion/css';
import { DataFrameView, GrafanaTheme2, dataFrameFromJSON } from '@grafana/data';
import { getBackendSrv } from '@grafana/runtime';
import { Badge, Icon, LinkButton, LoadingPlaceholder, Stack, useStyles2 } from '@grafana/ui';
import { lastValueFrom } from 'rxjs';
import { PLUGIN_ID } from '../../constants';
import { QueryKind } from '../../datasource/types';
import { sensorsUrl } from '../../scene-helpers/links';

interface SensorMetadataProps {
  serial: string;
}

interface DeviceRow {
  serial: string;
  name: string;
  mac: string;
  model: string;
  networkId: string;
  firmware: string;
  productType: string;
  tags: string;
  lanIp: string;
  address: string;
  lat: number;
  lng: number;
}

interface NetworkRow {
  id: string;
  name: string;
  timeZone: string;
  url: string;
}

interface SensorMetadataState {
  loading: boolean;
  error?: string;
  device?: DeviceRow;
  network?: NetworkRow;
}

/**
 * Sensor detail header — loads the device record and its network in two
 * sequential resource calls. Kept simple on purpose: no variable wiring,
 * no query runner. The scene header is mounted once per detail page so the
 * fetch cost is trivial.
 */
export function SensorMetadata({ serial }: SensorMetadataProps) {
  const styles = useStyles2(getStyles);
  const orgId = readOrgFromUrl();
  const [state, setState] = useState<SensorMetadataState>({ loading: true });

  useEffect(() => {
    let cancelled = false;

    (async () => {
      try {
        if (!orgId) {
          throw new Error('Select an organization from the Sensors page first.');
        }
        const device = await findDevice(orgId, serial);
        if (!device) {
          throw new Error(`No device with serial ${serial} in the selected organization.`);
        }
        const network = device.networkId ? await findNetwork(orgId, device.networkId) : undefined;
        if (cancelled) {
          return;
        }
        setState({ loading: false, device, network });
      } catch (e) {
        if (cancelled) {
          return;
        }
        setState({ loading: false, error: errorMessage(e) });
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [orgId, serial]);

  if (state.loading) {
    return (
      <div className={styles.wrap}>
        <LoadingPlaceholder text={`Loading sensor ${serial}…`} />
      </div>
    );
  }

  if (state.error || !state.device) {
    return (
      <div className={styles.wrap}>
        <Stack direction="column" gap={1}>
          <h3 className={styles.title}>{serial}</h3>
          <Badge color="red" text={state.error ?? 'Unknown error'} icon="exclamation-triangle" />
          <LinkButton icon="arrow-left" variant="secondary" href={sensorsUrl(orgId ?? undefined)}>
            Back to sensors
          </LinkButton>
        </Stack>
      </div>
    );
  }

  const d = state.device;
  const tags = d.tags ? d.tags.split(',').map((t) => t.trim()).filter((t) => t.length > 0) : [];

  return (
    <div className={styles.wrap}>
      <Stack direction="column" gap={1}>
        <div className={styles.header}>
          <h3 className={styles.title}>
            <Icon name="cube" className={styles.titleIcon} />
            {d.name || d.serial}
          </h3>
          <Stack direction="row" gap={1}>
            <Badge color="blue" text={d.model} />
            <Badge color="purple" text={d.productType} />
            {tags.map((t) => (
              <Badge key={t} color="darkgrey" text={t} />
            ))}
          </Stack>
        </div>

        <dl className={styles.grid}>
          <Field label="Serial" value={d.serial} monospace />
          <Field label="MAC" value={d.mac || '—'} monospace />
          <Field label="Firmware" value={d.firmware || '—'} />
          <Field label="LAN IP" value={d.lanIp || '—'} monospace />
          <Field label="Network" value={state.network?.name ?? d.networkId ?? '—'} />
          <Field label="Time zone" value={state.network?.timeZone ?? '—'} />
          <Field label="Address" value={d.address || '—'} />
          <Field
            label="Coordinates"
            value={d.lat || d.lng ? `${d.lat.toFixed(4)}, ${d.lng.toFixed(4)}` : '—'}
          />
        </dl>
      </Stack>
    </div>
  );
}

function Field({ label, value, monospace }: { label: string; value: string; monospace?: boolean }) {
  const styles = useStyles2(getStyles);
  return (
    <div className={styles.field}>
      <dt className={styles.dt}>{label}</dt>
      <dd className={monospace ? styles.ddMono : styles.dd}>{value}</dd>
    </div>
  );
}

function readOrgFromUrl(): string | undefined {
  if (typeof window === 'undefined') {
    return undefined;
  }
  const raw = new URLSearchParams(window.location.search).get('var-org');
  return raw ?? undefined;
}

async function findDevice(orgId: string, serial: string): Promise<DeviceRow | undefined> {
  const res = await postQuery<DeviceRow>('query', {
    queries: [{ refId: 'A', kind: QueryKind.Devices, orgId, productTypes: ['sensor'] }],
  });
  return res.find((r) => r.serial === serial);
}

async function findNetwork(orgId: string, networkId: string): Promise<NetworkRow | undefined> {
  const res = await postQuery<NetworkRow>('query', {
    queries: [{ refId: 'A', kind: QueryKind.Networks, orgId }],
  });
  return res.find((r) => r.id === networkId);
}

async function postQuery<T>(
  path: 'query',
  body: { queries: Array<Record<string, unknown>> }
): Promise<T[]> {
  const url = `/api/plugins/${PLUGIN_ID}/resources/${path}`;
  const now = Date.now();
  const payload = {
    range: { from: now - 60 * 60 * 1000, to: now },
    maxDataPoints: 100,
    intervalMs: 1000,
    ...body,
  };
  const obs = getBackendSrv().fetch<{ frames: object[] }>({ url, method: 'POST', data: payload });
  const { data } = await lastValueFrom(obs);
  const frames = (data?.frames ?? []).map((f) => dataFrameFromJSON(f as never));
  const rows: T[] = [];
  for (const frame of frames) {
    const view = new DataFrameView<T & Record<string, unknown>>(frame);
    for (let i = 0; i < view.length; i++) {
      rows.push({ ...(view.get(i) as T) });
    }
  }
  return rows;
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

const getStyles = (theme: GrafanaTheme2) => ({
  wrap: css`
    padding: ${theme.spacing(2, 3)};
    border: 1px solid ${theme.colors.border.weak};
    border-radius: ${theme.shape.radius.default};
    background: ${theme.colors.background.secondary};
  `,
  header: css`
    display: flex;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: ${theme.spacing(2)};
  `,
  title: css`
    margin: 0;
    display: flex;
    align-items: center;
    gap: ${theme.spacing(1)};
  `,
  titleIcon: css`
    color: ${theme.colors.text.secondary};
  `,
  grid: css`
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
    gap: ${theme.spacing(1, 3)};
    margin: 0;
    padding: 0;
  `,
  field: css`
    display: flex;
    flex-direction: column;
    min-width: 0;
  `,
  dt: css`
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
    margin: 0 0 2px 0;
  `,
  dd: css`
    margin: 0;
    overflow: hidden;
    text-overflow: ellipsis;
  `,
  ddMono: css`
    margin: 0;
    font-family: ${theme.typography.fontFamilyMonospace};
    overflow: hidden;
    text-overflow: ellipsis;
  `,
});
