import type { AppJsonData } from '../types';
import { resolveRecordingTarget } from './trend-query';

describe('resolveRecordingTarget', () => {
  it('returns null when no jsonData', () => {
    expect(resolveRecordingTarget(undefined, 'availability', 'device-status-overview')).toBeNull();
  });

  it('returns null when recordings slice is absent', () => {
    const jd: AppJsonData = {};
    expect(resolveRecordingTarget(jd, 'availability', 'device-status-overview')).toBeNull();
  });

  it('returns null when targetDatasourceUid is unset', () => {
    const jd: AppJsonData = {
      recordings: {
        groups: {
          availability: { installed: true, rulesEnabled: { 'device-status-overview': true } },
        },
      },
    };
    expect(resolveRecordingTarget(jd, 'availability', 'device-status-overview')).toBeNull();
  });

  it('returns null when the group is not installed', () => {
    const jd: AppJsonData = {
      recordings: {
        targetDatasourceUid: 'prom-uid',
        groups: {
          availability: { installed: false, rulesEnabled: { 'device-status-overview': true } },
        },
      },
    };
    expect(resolveRecordingTarget(jd, 'availability', 'device-status-overview')).toBeNull();
  });

  it('returns null when the specific rule toggle is off', () => {
    const jd: AppJsonData = {
      recordings: {
        targetDatasourceUid: 'prom-uid',
        groups: {
          availability: { installed: true, rulesEnabled: { 'device-status-overview': false } },
        },
      },
    };
    expect(resolveRecordingTarget(jd, 'availability', 'device-status-overview')).toBeNull();
  });

  it('returns the target UID when feature is fully configured', () => {
    const jd: AppJsonData = {
      recordings: {
        targetDatasourceUid: 'my-prom-uid',
        groups: {
          availability: { installed: true, rulesEnabled: { 'device-status-overview': true } },
        },
      },
    };
    expect(resolveRecordingTarget(jd, 'availability', 'device-status-overview')).toBe('my-prom-uid');
  });

  it('returns null when asked about a rule that is not in rulesEnabled at all', () => {
    const jd: AppJsonData = {
      recordings: {
        targetDatasourceUid: 'my-prom-uid',
        groups: {
          availability: { installed: true, rulesEnabled: { 'some-other-rule': true } },
        },
      },
    };
    expect(resolveRecordingTarget(jd, 'availability', 'device-status-overview')).toBeNull();
  });
});
