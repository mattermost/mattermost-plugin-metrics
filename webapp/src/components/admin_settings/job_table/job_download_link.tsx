import React, {ReactElement} from 'react';

import type {Job} from '../types/types';

const JobDownloadLink = React.memo(({job, download, remove}: {job: Job, download: (id: string) => {}, remove: (id: string) => void}): ReactElement => {
    switch (job.status) {
    case 'success':
        return (
            <div key={job.id}>
                <a
                    onClick={() => {
                        download(job.id);
                    }}
                >
                    {'Download'}
                </a>
                {', '}
                <a
                    onClick={() => {
                        remove(job.id);
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
    case 'error':
        return (
            <a
                style={{color: 'red'}}
                onClick={() => {
                    remove(job.id);
                }}
            >
                {'Remove (failed)'}
            </a>
        );
    default:
        return <>{'--'}</>;
    }
});

export default JobDownloadLink;
