// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

export type JobStatus = 'pending' | 'in_progress' | 'error' | 'success';

export type Job = {
    id: string;
    status: JobStatus;
    create_at: number;
    min_t: number;
    max_t: number;
    dump_location: string;
};

export type TSDBStats = {
    min_t: number;
    max_t: number;
    num_series: number;
    num_samples: number;
}
