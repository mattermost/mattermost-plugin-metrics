// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React, {useEffect, useRef, useState} from 'react';
import uPlot from 'uplot';
import 'uplot/dist/uPlot.min.css';

import {QueryResult} from './types';

type Props = {
    results: QueryResult[];
    legends: string[][]; // legends[queryIdx][seriesIdx]
    unit?: string;
    height?: number;
    startTime?: number; // unix seconds — fixes x-axis to the selected time window
    endTime?: number; // unix seconds
    onTimeRangeSelect?: (start: number, end: number) => void;
};

type TooltipData = {
    left: number; // px from container left
    top: number; // px from container top
    time: number; // unix seconds
    entries: Array<{label: string; value: number | null | undefined; color: string}>;
};

export const SERIES_COLORS = [
    '#0073e6', '#e6920a', '#2ecc71', '#9b59b6',
    '#e74c3c', '#1abc9c', '#f39c12', '#3498db',
];

export function formatValue(unit: string | undefined, v: number | null | undefined): string {
    if (v == null) {
        return '-';
    }
    if (unit === 'bytes') {
        if (v >= 1e9) {
            return `${(v / 1e9).toFixed(1)} GB`;
        }
        if (v >= 1e6) {
            return `${(v / 1e6).toFixed(1)} MB`;
        }
        if (v >= 1e3) {
            return `${(v / 1e3).toFixed(1)} KB`;
        }
        return `${v.toFixed(0)} B`;
    }
    if (unit === '%') {
        return `${v.toFixed(1)}%`;
    }
    if (unit === 's') {
        if (v >= 1) {
            return `${v.toFixed(2)} s`;
        }
        if (v >= 0.001) {
            return `${(v * 1000).toFixed(1)} ms`;
        }
        return `${(v * 1_000_000).toFixed(0)} μs`;
    }
    if (v >= 1e6) {
        return `${(v / 1e6).toFixed(2)} M`;
    }
    if (v >= 1e3) {
        return `${(v / 1e3).toFixed(2)} k`;
    }
    if (v >= 10) {
        return v.toFixed(2);
    }
    return v.toPrecision(3);
}

function formatTimestamp(unix: number): string {
    const d = new Date(unix * 1000);
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
           `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

// Merge multiple Prometheus series (each with their own timestamps) into
// uPlot's column-major format with a shared timestamp axis.
function buildUplotData(results: QueryResult[]): uPlot.AlignedData {
    const tsSet = new Set<number>();

    for (const result of results) {
        for (const s of result.series ?? []) {
            for (const [ts] of s.values) {
                tsSet.add(ts);
            }
        }
    }

    if (tsSet.size === 0) {
        return [[]];
    }

    const timestamps = Array.from(tsSet).sort((a, b) => a - b);
    const tsIdx = new Map(timestamps.map((ts, i) => [ts, i]));

    const data: (number | null)[][] = [timestamps];
    for (const result of results) {
        for (const s of result.series ?? []) {
            const col: (number | null)[] = new Array(timestamps.length).fill(null);
            for (const [ts, v] of s.values) {
                const i = tsIdx.get(ts);
                if (i != null) {
                    col[i] = v;
                }
            }
            data.push(col);
        }
    }

    return data as uPlot.AlignedData;
}

function buildSeriesConfig(
    results: QueryResult[],
    legends: string[][],
    unit: string | undefined,
): uPlot.Series[] {
    const series: uPlot.Series[] = [{}]; // x-axis placeholder
    let colorIdx = 0;
    results.forEach((result, ri) => {
        (result.series ?? []).forEach((s, si) => {
            // eslint-disable-next-line no-underscore-dangle
            const label = legends[ri]?.[si] ?? s.metric?.__name__ ?? `series ${colorIdx + 1}`;
            series.push({
                label,
                stroke: SERIES_COLORS[colorIdx % SERIES_COLORS.length],
                width: 1.5,
                value: (_u, v) => formatValue(unit, v),
            });
            colorIdx++;
        });
    });
    return series;
}

export default function UPlotChart({results, legends, unit, height = 160, startTime, endTime, onTimeRangeSelect}: Props) {
    const wrapperRef = useRef<HTMLDivElement>(null);
    const containerRef = useRef<HTMLDivElement>(null);
    const plotRef = useRef<uPlot | null>(null);
    const [tooltip, setTooltip] = useState<TooltipData | null>(null);

    // Use refs so hooks always have latest values without triggering chart rebuilds.
    const setTooltipRef = useRef(setTooltip);
    setTooltipRef.current = setTooltip;

    const onTimeRangeSelectRef = useRef(onTimeRangeSelect);
    onTimeRangeSelectRef.current = onTimeRangeSelect;

    const unitRef = useRef(unit);
    unitRef.current = unit;

    const hasData = results.some((r) => (r.series ?? []).length > 0);

    useEffect(() => {
        if (!containerRef.current || !hasData) {
            return () => {
                // no plot to clean up
            };
        }

        const data = buildUplotData(results);
        const series = buildSeriesConfig(results, legends, unit);

        const opts: uPlot.Options = {
            width: containerRef.current.clientWidth || 400,
            height,
            series,
            ...(startTime && endTime ? {scales: {x: {range: [startTime, endTime] as [number, number]}}} : {}),
            axes: [
                {
                    stroke: '#666',
                    grid: {stroke: '#e0e0e0', width: 1},
                },
                {
                    stroke: '#666',
                    grid: {stroke: '#e0e0e0', width: 1},
                    values: (_u, vals) => vals.map((v) => (v == null ? '' : formatValue(unit, v))),
                    size: 60,
                },
            ],
            cursor: {drag: {x: true, y: false}},
            legend: {show: true, live: false},
            padding: [8, 0, 0, 0],
            hooks: {
                setSelect: [(u) => {
                    const cb = onTimeRangeSelectRef.current;
                    if (!cb || u.select.width <= 0) {
                        return;
                    }
                    const selStart = u.posToVal(u.select.left, 'x');
                    const selEnd = u.posToVal(u.select.left + u.select.width, 'x');
                    if (selEnd > selStart) {
                        cb(Math.round(selStart), Math.round(selEnd));
                    }
                    u.setSelect({left: 0, top: 0, width: 0, height: 0}, false);
                }],
                setCursor: [(u) => {
                    const idx = u.cursor.idx;
                    if (idx == null || idx < 0) {
                        setTooltipRef.current(null);
                        return;
                    }

                    const time = (u.data[0] as number[])[idx];
                    const entries = u.series.slice(1).map((s, i) => ({
                        label: typeof s.label === 'string' ? s.label : '',
                        value: (u.data[i + 1] as (number | null)[])[idx],
                        color: typeof s.stroke === 'string' ? s.stroke : SERIES_COLORS[i % SERIES_COLORS.length],
                    }));

                    // Position relative to the wrapper div, offset from the
                    // plot's interactive overlay (u.over).
                    const overLeft = u.over.offsetLeft;
                    const overTop = u.over.offsetTop;
                    const cursorLeft = u.cursor.left ?? 0;
                    const cursorTop = u.cursor.top ?? 0;

                    setTooltipRef.current({
                        left: overLeft + cursorLeft,
                        top: overTop + cursorTop,
                        time,
                        entries,
                    });
                }],
                ready: [(u) => {
                    u.over.addEventListener('mouseleave', () => {
                        setTooltipRef.current(null);
                    });
                }],
            },
        };

        // eslint-disable-next-line new-cap
        plotRef.current = new uPlot(opts, data, containerRef.current);

        const ro = new ResizeObserver((entries) => {
            const w = entries[0]?.contentRect.width;
            if (w && plotRef.current) {
                plotRef.current.setSize({width: w, height});
            }
        });
        ro.observe(containerRef.current);

        return () => {
            ro.disconnect();
            plotRef.current?.destroy();
            plotRef.current = null;
            setTooltipRef.current(null);
        };
    }, [results, legends, unit, height, hasData]); // eslint-disable-line react-hooks/exhaustive-deps

    if (!hasData) {
        const errors = results.filter((r) => r.error).map((r) => r.error);
        return (
            <div
                style={{height, display: 'flex', alignItems: 'center', justifyContent: 'center', color: '#999', fontSize: 13}}
            >
                {errors.length > 0 ? errors[0] : 'No data'}
            </div>
        );
    }

    return (
        <div
            ref={wrapperRef}
            style={{position: 'relative'}}
        >
            <div ref={containerRef}/>
            {tooltip && (
                <Tooltip
                    data={tooltip}
                    unit={unit}
                    containerWidth={wrapperRef.current?.clientWidth ?? 0}
                />
            )}
        </div>
    );
}

const TOOLTIP_WIDTH = 260;
const TOOLTIP_OFFSET = 12;

function Tooltip({data, unit, containerWidth}: {data: TooltipData; unit: string | undefined; containerWidth: number}) {
    const flipLeft = data.left + TOOLTIP_OFFSET + TOOLTIP_WIDTH > containerWidth;
    const left = flipLeft ?
        data.left - TOOLTIP_OFFSET :
        data.left + TOOLTIP_OFFSET;
    const transform = flipLeft ? 'translate(-100%, -50%)' : 'translateY(-50%)';

    return (
        <div
            style={{
                position: 'absolute',
                left,
                top: data.top,
                transform,
                background: 'rgba(20, 22, 25, 0.92)',
                color: '#e0e0e0',
                borderRadius: 4,
                padding: '8px 10px',
                fontSize: 12,
                pointerEvents: 'none',
                zIndex: 9999,
                whiteSpace: 'nowrap',
                boxShadow: '0 2px 8px rgba(0,0,0,0.3)',
                minWidth: TOOLTIP_WIDTH,
            }}
        >
            <div style={{fontWeight: 600, marginBottom: 6, color: '#fff'}}>
                {formatTimestamp(data.time)}
            </div>
            {[...data.entries].sort((a, b) => {
                if (a.value == null && b.value == null) {
                    return 0;
                }
                if (a.value == null) {
                    return 1;
                }
                if (b.value == null) {
                    return -1;
                }
                return b.value - a.value;
            }).map((e, i) => (
                <div
                    key={i} // eslint-disable-line react/no-array-index-key
                    style={{display: 'flex', alignItems: 'center', gap: 6, marginBottom: 2}}
                >
                    <span
                        style={{
                            display: 'inline-block',
                            width: 10,
                            height: 10,
                            borderRadius: 2,
                            background: e.color,
                            flexShrink: 0,
                        }}
                    />
                    <span style={{flex: 1, color: '#ccc'}}>{e.label}</span>
                    <span style={{fontWeight: 600, color: '#fff', minWidth: 60, textAlign: 'right'}}>
                        {formatValue(unit, e.value)}
                    </span>
                </div>
            ))}
        </div>
    );
}
