import React, {ReactElement} from 'react';

import type {Job} from '../types/types';

import {deleteJob, downloadJob} from '../actions/actions';

const JobDownloadLink = React.memo(({job, cb}: {job: Job, cb: () => {}}): ReactElement => {
    switch (job.status) {
    case 'success':
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
    case 'pending':
        return (
            <div
                key={job.id}
                style={{color: 'orange'}}
            >
                {'Scheduled'}
            </div>
        );
    case 'in_progress':
        return (
            <div
                key={job.id}
                style={{color: 'green'}}
            >
                {'In progress..'}
            </div>
        );
    default:
        return <>{'--'}</>;
    }
});

export default JobDownloadLink;
