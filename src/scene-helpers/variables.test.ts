import { QueryVariable, TextBoxVariable } from '@grafana/scenes';
import { MERAKI_DS_REF } from './datasource';
import { clientVariable, networkVariable, orgVariable } from './variables';
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

describe('clientVariable', () => {
  it('is a TextBoxVariable so operators can paste any MAC', () => {
    // CustomVariable renders a fixed-options dropdown; with an empty `query`
    // the picker has nothing to select and no way to type. TextBoxVariable
    // renders a free-form text input, which is what the Clients Search tab
    // and per-client drilldown actually need.
    const v = clientVariable({ name: 'client', label: 'MAC' });

    expect(v).toBeInstanceOf(TextBoxVariable);
    expect(v.state.name).toBe('client');
    expect(v.state.label).toBe('MAC');
    expect(v.state.value).toBe('');
  });
});
