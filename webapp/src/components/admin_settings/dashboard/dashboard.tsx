// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react';

import {queryBatch} from '../actions/actions';
import {ActiveTimeRange, QueryResult, QUICK_RANGES} from './types';
import {SECTIONS, PANELS, buildQueryList} from './panels';
import UPlotChart from './uplot_chart';
import TimeRangePicker from './time_range_picker';
import './dashboard.scss';

const DEFAULT_RANGE: ActiveTimeRange = {
    label: QUICK_RANGES[3].label, // Last 1 hour
    step: QUICK_RANGES[3].step,
    relative: true,
    seconds: QUICK_RANGES[3].seconds,
    start: 0,
    end: 0,
};

type State = {
    results: QueryResult[];
    loading: boolean;
    error: string;
    activeRange: ActiveTimeRange;
    lastUpdated: Date | null;
    collapsed: Set<string>;
    windowStart: number;
    windowEnd: number;
};

export default class Dashboard extends React.PureComponent<Record<string, never>, State> {
    private refreshTimer: ReturnType<typeof setInterval> | null = null;
    private rootRef = React.createRef<HTMLDivElement>();
    private expandedEl: HTMLElement | null = null;
    private latestRequestId = 0;

    state: State = {
        results: [],
        loading: false,
        error: '',
        activeRange: DEFAULT_RANGE,
        lastUpdated: null,
        collapsed: new Set(),
        windowStart: 0,
        windowEnd: 0,
    };

    componentDidMount() {
        this.expandContainer();
        this.fetchData();
        this.refreshTimer = setInterval(this.fetchData, 60_000);
    }

    componentWillUnmount() {
        if (this.refreshTimer) {
            clearInterval(this.refreshTimer);
        }
        if (this.expandedEl) {
            this.expandedEl.style.maxWidth = '';
            this.expandedEl = null;
        }
    }

    private expandContainer() {
        let el = this.rootRef.current?.parentElement ?? null;
        while (el) {
            const maxWidth = parseFloat(window.getComputedStyle(el).maxWidth);
            if (maxWidth > 0 && maxWidth < window.innerWidth) {
                el.style.maxWidth = 'none';
                this.expandedEl = el;
                return;
            }
            el = el.parentElement;
        }
    }

    private fetchData = async () => {
        const requestId = ++this.latestRequestId;
        const {activeRange} = this.state;
        const end = activeRange.relative ? Math.floor(Date.now() / 1000) : activeRange.end;
        const start = activeRange.relative ? end - activeRange.seconds : activeRange.start;
        const {expressions} = buildQueryList();

        this.setState({loading: true, error: '', windowStart: start, windowEnd: end});
        try {
            const results = await queryBatch({start, end, step: activeRange.step, queries: expressions});
            if (requestId !== this.latestRequestId) {
                return;
            }
            this.setState({results, loading: false, lastUpdated: new Date()});
        } catch (e) {
            if (requestId !== this.latestRequestId) {
                return;
            }
            this.setState({loading: false, error: String(e)});
        }
    };

    private handleRangeChange = (range: ActiveTimeRange) => {
        this.setState({activeRange: range, results: []}, this.fetchData);
    };

    private toggleSection = (title: string) => {
        this.setState((prev) => {
            const collapsed = new Set(prev.collapsed);
            if (collapsed.has(title)) {
                collapsed.delete(title);
            } else {
                collapsed.add(title);
            }
            return {collapsed};
        });
    };

    render() {
        const {results, loading, error, activeRange, lastUpdated, collapsed, windowStart, windowEnd} = this.state;
        const {index} = buildQueryList();

        // Build per-panel result arrays (indexed against the flat PANELS list)
        const panelResults: QueryResult[][] = PANELS.map(() => []);
        const panelLegends: string[][][] = PANELS.map(() => []);

        index.forEach(({panelIdx, queryIdx}, flatIdx) => {
            const result = results[flatIdx];
            if (!result) {
                return;
            }
            panelResults[panelIdx][queryIdx] = result;
            panelLegends[panelIdx][queryIdx] = (result.series ?? []).map(
                (s) => PANELS[panelIdx].queries[queryIdx].legend(s.metric),
            );
        });

        // Track cumulative panel index across sections
        let panelOffset = 0;

        return (
            <div
                ref={this.rootRef}
                className='mm-dashboard'
            >
                <div className='mm-dashboard__toolbar'>
                    <TimeRangePicker
                        value={activeRange}
                        onChange={this.handleRangeChange}
                    />
                    <button
                        type='button'
                        className='btn btn-default mm-dashboard__refresh-btn'
                        onClick={this.fetchData}
                        disabled={loading}
                    >
                        {loading ? 'Loading…' : 'Refresh'}
                    </button>
                    {lastUpdated && (
                        <span className='mm-dashboard__updated'>
                            {`Updated ${lastUpdated.toLocaleTimeString()}`}
                        </span>
                    )}
                </div>

                {error && (
                    <div className='alert alert-danger mm-dashboard__error'>
                        {error}
                    </div>
                )}

                {SECTIONS.map((section) => {
                    const isCollapsed = collapsed.has(section.title);
                    const sectionOffset = panelOffset;
                    panelOffset += section.panels.length;

                    return (
                        <div
                            key={section.title}
                            className='mm-dashboard__section'
                        >
                            <button
                                type='button'
                                className='mm-dashboard__section-header'
                                onClick={() => this.toggleSection(section.title)}
                                aria-expanded={!isCollapsed}
                            >
                                <span className={`mm-dashboard__section-chevron${isCollapsed ? '' : ' mm-dashboard__section-chevron--open'}`}>
                                    {'›'}
                                </span>
                                {section.title}
                            </button>

                            {!isCollapsed && (
                                <div className='mm-dashboard__grid'>
                                    {section.panels.map((panel, i) => {
                                        const absIdx = sectionOffset + i;
                                        return (
                                            <div
                                                key={panel.title}
                                                className='mm-dashboard__panel'
                                            >
                                                <div className='mm-dashboard__panel-title'>{panel.title}</div>
                                                <UPlotChart
                                                    results={panelResults[absIdx].filter(Boolean)}
                                                    legends={panelLegends[absIdx]}
                                                    unit={panel.unit}
                                                    startTime={windowStart || undefined}
                                                    endTime={windowEnd || undefined}
                                                />
                                            </div>
                                        );
                                    })}
                                </div>
                            )}
                        </div>
                    );
                })}
            </div>
        );
    }
}
