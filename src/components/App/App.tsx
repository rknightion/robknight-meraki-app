import React from 'react';
import { SceneApp, SceneAppPageLike, useSceneApp } from '@grafana/scenes';
import { AppRootProps } from '@grafana/data';
import { PluginPropsContext } from '../../utils/utils.plugin';
import { homePage } from '../../pages/Home/homePage';
import { organizationsPage } from '../../pages/Organizations/organizationsPage';
import { appliancesPage } from '../../pages/Appliances/appliancesPage';
import { accessPointsPage } from '../../pages/AccessPoints/accessPointsPage';
import { switchesPage } from '../../pages/Switches/switchesPage';
import { camerasPage } from '../../pages/Cameras/camerasPage';
import { cellularGatewaysPage } from '../../pages/CellularGateways/cellularGatewaysPage';
import { sensorsPage } from '../../pages/Sensors/sensorsPage';
import { insightsPage } from '../../pages/Insights/insightsPage';
import { eventsPage } from '../../pages/Events/eventsPage';
import { alertsPage } from '../../pages/Alerts/alertsPage';
import { trafficPage } from '../../pages/Traffic/trafficPage';
import { topologyPage } from '../../pages/Topology/topologyPage';
import { auditLogPage } from '../../pages/AuditLog/auditLogPage';
import { clientsPage } from '../../pages/Clients/clientsPage';
import { firmwarePage } from '../../pages/Firmware/firmwarePage';
import { configurationPage } from '../../pages/Configuration/configurationPage';

/**
 * Full list of scene pages. Device-family pages (Appliances / Access Points
 * / Switches / Cameras / Cellular Gateways / Sensors) are currently always
 * rendered; a `showEmptyFamilies=false` auto-hide mode is wired on the
 * backend (`KindOrgProductTypes`) and the Configuration form but kept out
 * of the nav until we find a way to swap the `SceneApp.pages` list without
 * tearing down in-flight scene runners.
 */
function allPages(): SceneAppPageLike[] {
  return [
    homePage,
    organizationsPage,
    appliancesPage,
    accessPointsPage,
    switchesPage,
    camerasPage,
    cellularGatewaysPage,
    sensorsPage,
    insightsPage,
    eventsPage,
    alertsPage,
    trafficPage,
    topologyPage,
    auditLogPage,
    clientsPage,
    firmwarePage,
    configurationPage,
  ];
}

function getSceneApp() {
  return new SceneApp({
    pages: allPages(),
    urlSyncOptions: {
      updateUrlOnInit: true,
      createBrowserHistorySteps: true,
    },
  });
}

function AppWithScenes() {
  const scene = useSceneApp(getSceneApp);
  return <scene.Component model={scene} />;
}

function App(props: AppRootProps) {
  return (
    <PluginPropsContext.Provider value={props}>
      <AppWithScenes />
    </PluginPropsContext.Provider>
  );
}

export default App;
