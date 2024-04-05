import {Client4} from 'mattermost-redux/client';

import {DateRange} from 'react-day-picker';

import {Job, TSDBStats} from '../types/types';
import {manifest} from '@/manifest';

export function getTSDBStats() {
    return Client4.doFetch<TSDBStats>(
        `${Client4.getUrl()}/plugins/${manifest.id}/tsdb`,
        {method: 'get'},
    );
}

export function getJobs() {
    return Client4.doFetch<Job[]>(
        `${Client4.getUrl()}/plugins/${manifest.id}/jobs`,
        {method: 'get'},
    );
}

export async function craeteJob(range: DateRange) {
    return Client4.doFetch(`${Client4.getUrl()}/plugins/${manifest.id}/jobs/create`, {
        method: 'post',
        headers: {'Content-Type': 'application/json'},
        body: `{"MinT": ${range?.from?.getTime()}, "MaxT": ${range?.to?.getTime()}}`,
    });
}

export function deleteJob(id: string) {
    return Client4.doFetch(`${Client4.getUrl()}/plugins/${manifest.id}/jobs/delete/${id}`, {
        method: 'delete',
    });
}

export async function downloadJob(id: string) {
    const res = await fetch(`${Client4.getUrl()}/plugins/${manifest.id}/jobs/download/${id}`, {
        method: 'get',
    });
    const blob = await res.blob();
    const href = window.URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = href;
    link.setAttribute('download', extractFilename(res.headers.get('content-disposition')));
    document.body.appendChild(link);
    link.click();
    document.body.removeChild(link);
}

function extractFilename(input: string | null): string {
    const presumedFileName = 'tsdb_dump.tar.gz';
    if (input === null) {
        return presumedFileName;
    }

    const regex = /filename\*?=["']?((?:\\.|[^"'\s])+)(?=["']?)/g;
    const matches = regex.exec(input!);

    return matches ? matches[1] : presumedFileName;
}
