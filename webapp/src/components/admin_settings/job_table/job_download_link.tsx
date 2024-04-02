import React, {ReactElement} from 'react';

import type {Job} from '../types/types';

import {deleteJob, downloadJob} from './actions';

const JobDownloadLink = React.memo(({job, cb}: {job: Job, cb: () => {}}): ReactElement => {
    if (job.status === 'success') {
        return (
            <div key={job.id}>
                <a
                    onClick={() => {
                        downloadJob(job.id);
                    }}
                >
                    {'Download'}
                </a>
                {', '}
                <a
                    onClick={() => {
                        deleteJob(job.id);
                        cb();
                    }}
                >
                    {'Remove'}
                </a>
            </div>
        );
    } else if (job.status === 'scheduled') {
        return (
            <div
                key={job.id}
                style={{color: 'orange'}}
            >
                {'Scheduled'}
            </div>
        );
    }

    return <>{'--'}</>;
});

export default JobDownloadLink;
