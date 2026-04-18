import React from 'react';
import { SceneApp, useSceneApp } from '@grafana/scenes';
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
import { configurationPage } from '../../pages/Configuration/configurationPage';

function getSceneApp() {
  return new SceneApp({
    pages: [
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
      configurationPage,
    ],
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
