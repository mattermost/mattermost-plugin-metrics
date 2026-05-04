// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {Client4} from 'mattermost-redux/client';

import type {Options} from '@mattermost/types/client4';
import {DateRange} from 'react-day-picker';

import {Job, TSDBStats} from '../types/types';
import {BatchQueryRequest, QueryResult} from '../dashboard/types';
import {manifest} from '@/manifest';

async function pluginFetch<T = void>(path: string, options: Options = {}): Promise<T> {
    const url = `${Client4.getUrl()}/plugins/${manifest.id}${path}`;
    const res = await fetch(url, Client4.getOptions(options));
    if (!res.ok) {
        throw new Error(`${res.status} ${res.statusText}`);
    }
    if (res.status === 204) {
        return null as T;
    }
    const text = await res.text();
    if (text.trim() === '') {
        return null as T;
    }
    return JSON.parse(text) as T;
}

export function getTSDBStats() {
    return pluginFetch<TSDBStats>('/tsdb/stats', {method: 'get'});
}

export function getJobs() {
    return pluginFetch<Job[]>('/jobs', {method: 'get'});
}

export async function createJob(range: DateRange) {
    return pluginFetch('/jobs/create', {
        method: 'post',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({min_t: range.from?.getTime(), max_t: range.to?.getTime()}),
    });
}

export function deleteJob(id: string) {
    return pluginFetch(`/jobs/delete/${id}`, {method: 'delete'});
}

export async function deleteAllJobs() {
    return pluginFetch('/jobs/deleteAll', {method: 'delete'});
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

export function queryBatch(req: BatchQueryRequest): Promise<QueryResult[]> {
    return pluginFetch<QueryResult[]>('/metrics/query_batch', {
        method: 'post',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify(req),
    });
}

function extractFilename(input: string | null): string {
    const presumedFileName = 'tsdb_dump.tar.gz';
    if (input === null) {
        return presumedFileName;
    }

    const regex = /filename\*?=["']?((?:\\.|[^"'\s])+)(?=["']?)/g;
    const matches = regex.exec(input);

    return matches ? matches[1] : presumedFileName;
}
