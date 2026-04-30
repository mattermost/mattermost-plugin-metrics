// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import {PanelDef, SectionDef} from './types';

const byLabel = (name: string) => (m: Record<string, string>) => m[name] || name;
const fixed = (label: string) => () => label;

export const SECTIONS: SectionDef[] = [
    {
        title: 'HTTP / API',
        panels: [
            {
                title: 'HTTP Requests/s',
                queries: [
                    {expr: 'sum(rate(mattermost_http_requests_total[2m]))', legend: fixed('total')},
                ],
                unit: 'req/s',
            },
            {
                title: 'API Latency',
                queries: [
                    {
                        expr: 'histogram_quantile(0.99, sum(rate(mattermost_api_time_bucket[2m])) by (le))',
                        legend: fixed('p99'),
                    },
                    {
                        expr: 'histogram_quantile(0.50, sum(rate(mattermost_api_time_bucket[2m])) by (le))',
                        legend: fixed('p50'),
                    },
                ],
                unit: 's',
            },
            {
                title: 'Top 10 API Requests by Count',
                queries: [
                    {
                        expr: 'topk(10, sum(rate(mattermost_api_time_count[5m])) by (handler))',
                        legend: byLabel('handler'),
                    },
                ],
                unit: 'req/s',
            },
            {
                title: 'Top 10 API Requests by Duration',
                queries: [
                    {
                        expr: 'topk(10, sum(rate(mattermost_api_time_sum[5m])) by (handler) / sum(rate(mattermost_api_time_count[5m])) by (handler))',
                        legend: byLabel('handler'),
                    },
                ],
                unit: 's',
            },
            {
                title: 'Channel Load Duration',
                queries: [
                    {
                        expr: 'histogram_quantile(0.99, sum(rate(mattermost_api_time_bucket{handler="getPostsForChannelAroundLastUnread"}[5m])) by (le))',
                        legend: fixed('p99'),
                    },
                    {
                        expr: 'histogram_quantile(0.50, sum(rate(mattermost_api_time_bucket{handler="getPostsForChannelAroundLastUnread"}[5m])) by (le))',
                        legend: fixed('p50'),
                    },
                ],
                unit: 's',
            },
            {
                title: 'Create Post Duration',
                queries: [
                    {
                        expr: 'histogram_quantile(0.99, sum(rate(mattermost_api_time_bucket{handler="createPost"}[5m])) by (le))',
                        legend: fixed('p99'),
                    },
                    {
                        expr: 'histogram_quantile(0.50, sum(rate(mattermost_api_time_bucket{handler="createPost"}[5m])) by (le))',
                        legend: fixed('p50'),
                    },
                ],
                unit: 's',
            },
        ],
    },
    {
        title: 'Database',
        panels: [
            {
                title: 'DB Calls/s',
                queries: [
                    {
                        expr: 'sum(rate(mattermost_db_store_time_count[5m]))',
                        legend: fixed('total'),
                    },
                ],
                unit: 'calls/s',
            },
            {
                title: 'Store Latency',
                queries: [
                    {
                        expr: 'histogram_quantile(0.99, sum(rate(mattermost_db_store_time_bucket[2m])) by (le))',
                        legend: fixed('p99'),
                    },
                    {
                        expr: 'histogram_quantile(0.50, sum(rate(mattermost_db_store_time_bucket[2m])) by (le))',
                        legend: fixed('p50'),
                    },
                ],
                unit: 's',
            },
            {
                title: 'Top 10 DB Calls by Count',
                queries: [
                    {
                        expr: 'topk(10, sum(rate(mattermost_db_store_time_count[5m])) by (method))',
                        legend: byLabel('method'),
                    },
                ],
                unit: 'calls/s',
            },
            {
                title: 'Top 10 DB Calls by Duration',
                queries: [
                    {
                        expr: 'topk(10, sum(rate(mattermost_db_store_time_sum[5m])) by (method) / sum(rate(mattermost_db_store_time_count[5m])) by (method))',
                        legend: byLabel('method'),
                    },
                ],
                unit: 's',
            },
            {
                title: 'DB Connections',
                queries: [
                    {
                        expr: 'sum(mattermost_db_master_connections_total)',
                        legend: fixed('master'),
                    },
                    {
                        expr: 'sum(mattermost_db_read_replica_connections_total)',
                        legend: fixed('replica'),
                    },
                ],
            },
            {
                title: 'Connection Pool (Master)',
                queries: [
                    {expr: 'sum(go_sql_open_connections{db_name="master"})', legend: fixed('open')},
                    {expr: 'sum(go_sql_in_use_connections{db_name="master"})', legend: fixed('in use')},
                    {
                        expr: 'sum(rate(go_sql_wait_count_total{db_name="master"}[5m]))',
                        legend: fixed('waiting'),
                    },
                ],
            },
            {
                title: 'Connection Pool (Replica)',
                queries: [
                    {
                        expr: 'sum(go_sql_open_connections{db_name=~"replica.*"})',
                        legend: fixed('open'),
                    },
                    {
                        expr: 'sum(go_sql_in_use_connections{db_name=~"replica.*"})',
                        legend: fixed('in use'),
                    },
                    {
                        expr: 'sum(rate(go_sql_wait_count_total{db_name=~"replica.*"}[5m]))',
                        legend: fixed('waiting'),
                    },
                ],
            },
            {
                title: 'Replica Lag',
                queries: [
                    {expr: 'mattermost_db_replica_lag_time', legend: byLabel('instance')},
                ],
                unit: 's',
            },
        ],
    },
    {
        title: 'WebSocket',
        panels: [
            {
                title: 'WebSocket Connections',
                queries: [
                    {expr: 'sum(mattermost_http_websockets_total)', legend: fixed('total')},
                ],
            },
            {
                title: 'WebSocket Buffer Size',
                queries: [
                    {
                        expr: 'sum(mattermost_websocket_broadcast_buffer_size)',
                        legend: fixed('total'),
                    },
                ],
            },
            {
                title: 'WebSocket Broadcasts/s',
                queries: [
                    {
                        expr: 'sum(rate(mattermost_websocket_broadcasts_total[5m])) by (name)',
                        legend: byLabel('name'),
                    },
                ],
                unit: 'msg/s',
            },
            {
                title: 'WebSocket Events/s',
                queries: [
                    {
                        expr: 'sum(rate(mattermost_websocket_event_total[5m])) by (type)',
                        legend: byLabel('type'),
                    },
                ],
                unit: 'events/s',
            },
            {
                title: 'WebSocket Reconnects',
                queries: [
                    {
                        expr: 'sum(increase(mattermost_websocket_reconnects_total[10m]))',
                        legend: fixed('total'),
                    },
                ],
            },
        ],
    },
    {
        title: 'Cluster',
        panels: [
            {
                title: 'Cluster Health Score',
                queries: [
                    {
                        expr: 'mattermost_cluster_cluster_health_score',
                        legend: byLabel('instance'),
                    },
                ],
            },
            {
                title: 'Cluster Request Duration',
                queries: [
                    {
                        expr: 'histogram_quantile(0.99, sum(rate(mattermost_cluster_cluster_request_duration_seconds_bucket[5m])) by (le))',
                        legend: fixed('p99'),
                    },
                    {
                        expr: 'histogram_quantile(0.50, sum(rate(mattermost_cluster_cluster_request_duration_seconds_bucket[5m])) by (le))',
                        legend: fixed('p50'),
                    },
                ],
                unit: 's',
            },
            {
                title: 'Cluster Requests/s',
                queries: [
                    {
                        expr: 'sum(rate(mattermost_cluster_cluster_request_duration_seconds_count[5m]))',
                        legend: fixed('total'),
                    },
                ],
                unit: 'req/s',
            },
        ],
    },
    {
        title: 'Jobs',
        panels: [
            {
                title: 'Active Jobs',
                queries: [
                    {expr: 'sum(mattermost_jobs_active)', legend: fixed('total')},
                ],
            },
        ],
    },
    {
        title: 'Process / Runtime',
        panels: [
            {
                title: 'CPU Utilization',
                queries: [
                    {
                        expr: 'irate(mattermost_process_cpu_seconds_total[5m]) * 100',
                        legend: byLabel('instance'),
                    },
                ],
                unit: '%',
            },
            {
                title: 'Heap In Use',
                queries: [
                    {expr: 'go_memstats_heap_inuse_bytes', legend: byLabel('instance')},
                ],
                unit: 'bytes',
            },
            {
                title: 'Goroutines',
                queries: [
                    {expr: 'sum(go_goroutines)', legend: fixed('total')},
                ],
            },
        ],
    },
];

// Flat list of all panels for backwards-compatible batch query building.
export const PANELS: PanelDef[] = SECTIONS.flatMap((s) => s.panels);

export type PanelQueryIndex = {panelIdx: number; queryIdx: number};

export function buildQueryList(): {expressions: string[]; index: PanelQueryIndex[]} {
    const expressions: string[] = [];
    const index: PanelQueryIndex[] = [];
    PANELS.forEach((panel, panelIdx) => {
        panel.queries.forEach((q, queryIdx) => {
            expressions.push(q.expr);
            index.push({panelIdx, queryIdx});
        });
    });
    return {expressions, index};
}
