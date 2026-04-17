import { DataSourcePlugin } from '@grafana/data';
import { MerakiDataSource } from './datasource';
import { ConfigEditor } from './ConfigEditor';
import { QueryEditor } from './QueryEditor';
import { MerakiDSOptions, MerakiQuery } from './types';

export const plugin = new DataSourcePlugin<MerakiDataSource, MerakiQuery, MerakiDSOptions>(
  MerakiDataSource
)
  .setConfigEditor(ConfigEditor)
  .setQueryEditor(QueryEditor);
