import { CustomVariable } from '@grafana/scenes';
import { licenseStateVariable } from './variables';

describe('licenseStateVariable', () => {
  it('is a CustomVariable named "licenseState" that defaults to "All"', () => {
    const v = licenseStateVariable();

    expect(v).toBeInstanceOf(CustomVariable);
    expect(v.state.name).toBe('licenseState');
    expect(v.state.label).toBe('State');
    expect(v.state.includeAll).toBe(true);
    expect(v.state.defaultToAll).toBe(true);
    expect(v.state.isMulti).toBe(false);
    // "All" sentinel maps to the empty string so the backend skips the
    // state filter entirely.
    expect(v.state.allValue).toBe('');
  });

  it('offers the six Meraki license states plus an All option', () => {
    const v = licenseStateVariable();
    const query = v.state.query;
    expect(typeof query).toBe('string');

    const spec = query as string;
    // The query spec uses pipe/comma-delimited `label : value` entries.
    // Split on commas, trim, and extract values (everything after the `:`
    // for the leading `All : ,` entry, just the literal string otherwise).
    const entries = spec.split(',').map((s) => s.trim());
    // ['All : ', 'active', 'expired', 'expiring', 'recentlyQueued', 'unused', 'unusedActive']
    expect(entries).toHaveLength(7);

    // Entry 0 is the All sentinel, values 1..6 are the six Meraki states.
    expect(entries[0]).toBe('All :');
    expect(entries.slice(1)).toEqual([
      'active',
      'expired',
      'expiring',
      'recentlyQueued',
      'unused',
      'unusedActive',
    ]);
  });
});
