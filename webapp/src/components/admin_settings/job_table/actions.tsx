import {Client4} from 'mattermost-redux/client';

import {Job} from '../types/types';
import {manifest} from '@/manifest';

export function getJobs() {
    return Client4.doFetch<Job[]>(
        `${Client4.getUrl()}/plugins/${manifest.id}/jobs`,
        {method: 'get'},
    );
}
