import { QueryVariable } from '@grafana/scenes';
import { MERAKI_DS_REF } from './datasource';
import { networkVariable, orgVariable } from './variables';
import { QueryKind } from '../datasource/types';

describe('orgVariable', () => {
  it('is a QueryVariable bound to the Meraki data source', () => {
    const v = orgVariable();

    expect(v).toBeInstanceOf(QueryVariable);
    expect(v.state.name).toBe('org');
    expect(v.state.datasource).toEqual(MERAKI_DS_REF);
  });

  it('queries the Organizations kind', () => {
    const v = orgVariable();
    const query = v.state.query as { kind?: string };

    expect(query.kind).toBe(QueryKind.Organizations);
  });
});

describe('networkVariable', () => {
  it('is a multi-select variable that defaults to all networks', () => {
    const v = networkVariable();

    expect(v).toBeInstanceOf(QueryVariable);
    expect(v.state.isMulti).toBe(true);
    expect(v.state.defaultToAll).toBe(true);
  });

  it('cascades from $org', () => {
    const v = networkVariable();
    const query = v.state.query as { kind?: string; orgId?: string };

    expect(query.kind).toBe(QueryKind.Networks);
    expect(query.orgId).toBe('$org');
  });
});
