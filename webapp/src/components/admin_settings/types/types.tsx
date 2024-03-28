export type JobStatus = 'scheduled' | 'failed' | 'success';

export type Job = {
    id: string;
    status: JobStatus;
    create_at: number;
    min_t: number;
    max_t: number;
    dump_location: string;
};
