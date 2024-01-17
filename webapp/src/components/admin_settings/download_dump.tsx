import React from 'react';

import {Client4} from 'mattermost-redux/client';

import {manifest} from '@/manifest';

export const DownloadDump = () => {
    return (
        <div>
            <div
                className='col-sm-13'
                style={styles.buttonRow}
            >
                <a
                    className='btn btn-primary'
                    href={`${Client4.getUrl()}/plugins/${manifest.id}/download`}
                    rel='noopener noreferrer'
                >
                    {'Download Dump'}
                </a>
            </div>
        </div>
    );
};

const styles = {
    buttonRow: {
        margin: '12px 0',
    },
};
