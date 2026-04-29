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

export type TimeRange = {
    label: string;
    seconds: number;
    step: number; // query step in seconds
};

export const TIME_RANGES: TimeRange[] = [
    {label: 'Last 15 min', seconds: 15 * 60, step: 60},
    {label: 'Last 1 hour', seconds: 60 * 60, step: 60},
    {label: 'Last 6 hours', seconds: 6 * 60 * 60, step: 60},
    {label: 'Last 24 hours', seconds: 24 * 60 * 60, step: 300},
    {label: 'Last 7 days', seconds: 7 * 24 * 60 * 60, step: 1800},
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
