export const testIds = {
  appConfig: {
    container: 'data-testid ac-container',
    apiKey: 'data-testid ac-api-key',
    region: 'data-testid ac-region',
    baseUrl: 'data-testid ac-base-url',
    sharedFraction: 'data-testid ac-shared-fraction',
    labelMode: 'data-testid ac-label-mode',
    enableIPLimiter: 'data-testid ac-enable-ip-limiter',
    showEmptyFamilies: 'data-testid ac-show-empty-families',
    submit: 'data-testid ac-submit',
    testConnection: 'data-testid ac-test-connection',
    connectionResult: 'data-testid ac-connection-result',
  },
  alertRulesPanel: {
    container: 'data-testid arp-container',
    featureToggleBanner: 'data-testid arp-feature-toggle-banner',
    driftBanner: 'data-testid arp-drift-banner',
    resultBanner: 'data-testid arp-result-banner',
    statusPill: 'data-testid arp-status-pill',
    reconcileButton: 'data-testid arp-reconcile',
    uninstallButton: 'data-testid arp-uninstall',
    uninstallConfirm: 'data-testid arp-uninstall-confirm',
    viewInGrafana: 'data-testid arp-view-in-grafana',
    groupCard: (groupId: string) => `data-testid arp-group-${groupId}`,
    groupInstallToggle: (groupId: string) => `data-testid arp-group-install-${groupId}`,
    templateRow: (groupId: string, templateId: string) =>
      `data-testid arp-template-${groupId}-${templateId}`,
    ruleEnabled: (groupId: string, templateId: string) =>
      `data-testid arp-rule-enabled-${groupId}-${templateId}`,
    thresholdInput: (groupId: string, templateId: string, key: string) =>
      `data-testid arp-threshold-${groupId}-${templateId}-${key}`,
  },
  home: {
    container: 'data-testid home-container',
    orgCountStat: 'data-testid home-org-count',
    deviceCountStat: 'data-testid home-device-count',
    statusPie: 'data-testid home-status-pie',
  },
  organizations: {
    container: 'data-testid organizations-container',
    table: 'data-testid organizations-table',
  },
  sensors: {
    container: 'data-testid sensors-container',
    timeseries: 'data-testid sensors-timeseries',
    latestTiles: 'data-testid sensors-latest-tiles',
  },
};
