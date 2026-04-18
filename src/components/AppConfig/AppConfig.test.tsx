import { regionForUrl } from './AppConfig';

describe('regionForUrl', () => {
  it('maps a blank baseUrl to Global / US (the default region)', () => {
    expect(regionForUrl('')).toBe('Global / US');
  });

  it('maps each known regional URL back to its label', () => {
    expect(regionForUrl('https://api.meraki.com/api/v1')).toBe('Global / US');
    expect(regionForUrl('https://api.meraki.ca/api/v1')).toBe('Canada');
    expect(regionForUrl('https://api.meraki.cn/api/v1')).toBe('China');
    expect(regionForUrl('https://api.meraki.in/api/v1')).toBe('India');
    expect(regionForUrl('https://api.gov-meraki.com/api/v1')).toBe('US Federal');
  });

  it('falls back to Custom… for unknown URLs so users can still edit the input', () => {
    expect(regionForUrl('https://api.meraki.example.internal/v1')).toBe('Custom…');
    expect(regionForUrl('https://sandbox.meraki.com/api/v1')).toBe('Custom…');
  });
});
