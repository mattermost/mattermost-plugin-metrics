import {Store, Action} from 'redux';

import {GlobalState} from '@mattermost/types/lib/store';

import {manifest} from '@/manifest';

import JobTable from './components/admin_settings/job_table/job_table';
import TSDBStatsTable from './components/admin_settings/tsdb_stats/tsdb_stats';

export default class Plugin {
    // eslint-disable-next-line @typescript-eslint/no-unused-vars, @typescript-eslint/no-empty-function
    public async initialize(registry: any, store: Store<GlobalState, Action<Record<string, unknown>>>) {
        registry.registerAdminConsoleCustomSetting('Stats', TSDBStatsTable);
        registry.registerAdminConsoleCustomSetting('Dumps', JobTable);
    }
}

declare global {
    interface Window {
        registerPlugin(pluginId: string, plugin: Plugin): void
    }
}

window.registerPlugin(manifest.id, new Plugin());
