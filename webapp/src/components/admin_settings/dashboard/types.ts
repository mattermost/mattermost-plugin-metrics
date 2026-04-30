// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

export type QuerySeries = {
    metric: Record<string, string>;
    values: [number, number][]; // [unix_seconds, value]
};

export type QueryResult = {
    query: string;
    series: QuerySeries[];
    error?: string;
};

export type BatchQueryRequest = {
    start: number; // unix seconds
    end: number;   // unix seconds
    step: number;  // seconds
    queries: string[];
};

export type QuickRange = {
    label: string;
    seconds: number;
    step: number;
};

export type ActiveTimeRange = {
    label: string;
    step: number;
    relative: boolean;
    seconds: number; // used when relative === true
    start: number;   // used when relative === false
    end: number;     // used when relative === false
};

export const QUICK_RANGES: QuickRange[] = [
    {label: 'Last 5 minutes', seconds: 5 * 60, step: 60},
    {label: 'Last 15 minutes', seconds: 15 * 60, step: 60},
    {label: 'Last 30 minutes', seconds: 30 * 60, step: 60},
    {label: 'Last 1 hour', seconds: 60 * 60, step: 60},
    {label: 'Last 3 hours', seconds: 3 * 60 * 60, step: 60},
    {label: 'Last 6 hours', seconds: 6 * 60 * 60, step: 300},
    {label: 'Last 12 hours', seconds: 12 * 60 * 60, step: 300},
    {label: 'Last 24 hours', seconds: 24 * 60 * 60, step: 300},
    {label: 'Last 2 days', seconds: 2 * 24 * 60 * 60, step: 300},
    {label: 'Last 7 days', seconds: 7 * 24 * 60 * 60, step: 1800},
    {label: 'Last 30 days', seconds: 30 * 24 * 60 * 60, step: 3600},
];

export type PanelQuery = {
    expr: string;
    legend: (metric: Record<string, string>) => string;
};

export type PanelDef = {
    title: string;
    queries: PanelQuery[];
    unit?: string;
};

export type SectionDef = {
    title: string;
    panels: PanelDef[];
};
