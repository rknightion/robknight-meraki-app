import { deviceDrilldownUrl } from './links';

describe('deviceDrilldownUrl', () => {
  test.each([
    ['wireless', 'access-points'],
    ['switch', 'switches'],
    ['appliance', 'appliances'],
    ['camera', 'cameras'],
    ['cellularGateway', 'cellular-gateways'],
    ['sensor', 'sensors'],
    ['unknown', 'sensors'], // safe fallback — see comment in `links.ts::deviceDrilldownUrl`.
  ])('productType=%s routes to /%s/', (productType, route) => {
    const url = deviceDrilldownUrl(productType, 'Q2XX-YYYY-ZZZZ');
    expect(url).toContain('/' + route + '/');
    expect(url).toContain('Q2XX-YYYY-ZZZZ');
  });

  test('includes var-org query when orgId provided', () => {
    const url = deviceDrilldownUrl('wireless', 'X', 'O-123');
    expect(url).toContain('var-org=O-123');
  });

  test('omits var-org query when orgId absent', () => {
    const url = deviceDrilldownUrl('wireless', 'X');
    expect(url).not.toContain('var-org');
  });
});
