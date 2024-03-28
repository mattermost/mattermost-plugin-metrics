import React, {ReactElement} from 'react';

import {Client4} from 'mattermost-redux/client';

import type {Job} from '../types/types';

const JobDownloadLink = React.memo(({job}: {job: Job}): ReactElement => {
    if (job.status === 'success') {
        return (
            <div key={job.id}>
                <a
                    href={`${Client4.getUrl()}/plugins/com.mattermost.mattermost-plugin-metrics/jobs/download/${job.id}`}
                    rel='noopener noreferrer'
                >
                    {'Download'}
                </a>
                {', '}
                <a
                    href={`${Client4.getUrl()}/plugins/com.mattermost.mattermost-plugin-metrics/jobs/delete/${job.id}`}
                    rel='noopener noreferrer'
                >
                    {'Remove'}
                </a>
            </div>
        );
    }

    return <>{'--'}</>;
});

export default JobDownloadLink;
