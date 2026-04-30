// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React, {useEffect, useRef, useState} from 'react';

import {ActiveTimeRange, QuickRange, QUICK_RANGES} from './types';
import './time_range_picker.scss';

type Props = {
    value: ActiveTimeRange;
    onChange: (range: ActiveTimeRange) => void;
};

function toDatetimeLocal(unix: number): string {
    const d = new Date(unix * 1000);
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
}

function fromDatetimeLocal(s: string): number {
    return Math.floor(new Date(s).getTime() / 1000);
}

export default function TimeRangePicker({value, onChange}: Props) {
    const [open, setOpen] = useState(false);
    const [fromVal, setFromVal] = useState('');
    const [toVal, setToVal] = useState('');
    const [search, setSearch] = useState('');
    const [applyError, setApplyError] = useState('');
    const containerRef = useRef<HTMLDivElement>(null);

    const initAbsoluteInputs = () => {
        const now = Math.floor(Date.now() / 1000);
        const end = value.relative ? now : value.end;
        const start = value.relative ? now - value.seconds : value.start;
        setFromVal(toDatetimeLocal(start));
        setToVal(toDatetimeLocal(end));
        setApplyError('');
    };

    const handleOpen = () => {
        initAbsoluteInputs();
        setOpen(true);
    };

    useEffect(() => {
        if (!open) {
            return undefined;
        }
        const handleMouseDown = (e: MouseEvent) => {
            if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
                setOpen(false);
            }
        };
        const handleKeyDown = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                setOpen(false);
            }
        };
        document.addEventListener('mousedown', handleMouseDown);
        document.addEventListener('keydown', handleKeyDown);
        return () => {
            document.removeEventListener('mousedown', handleMouseDown);
            document.removeEventListener('keydown', handleKeyDown);
        };
    }, [open]);

    const handleQuickRange = (qr: QuickRange) => {
        onChange({
            label: qr.label,
            step: qr.step,
            relative: true,
            seconds: qr.seconds,
            start: 0,
            end: 0,
        });
        setOpen(false);
        setSearch('');
    };

    const handleApply = () => {
        const start = fromDatetimeLocal(fromVal);
        const end = fromDatetimeLocal(toVal);
        if (isNaN(start) || isNaN(end)) {
            setApplyError('Invalid date');
            return;
        }
        if (start >= end) {
            setApplyError('"From" must be before "To"');
            return;
        }
        const durationSeconds = end - start;
        let step = 60;
        if (durationSeconds > 7 * 86400) {
            step = 3600;
        } else if (durationSeconds > 86400) {
            step = 1800;
        } else if (durationSeconds > 3600) {
            step = 300;
        }
        onChange({
            label: `${fromVal.replace('T', ' ')} to ${toVal.replace('T', ' ')}`,
            step,
            relative: false,
            seconds: 0,
            start,
            end,
        });
        setOpen(false);
    };

    const filtered = QUICK_RANGES.filter(
        (qr) => search === '' || qr.label.toLowerCase().includes(search.toLowerCase()),
    );

    return (
        <div
            className='mm-trp'
            ref={containerRef}
        >
            <button
                type='button'
                className='mm-trp__trigger btn btn-default'
                onClick={() => (open ? setOpen(false) : handleOpen())}
                aria-haspopup='true'
                aria-expanded={open}
            >
                <i className='icon icon-clock-outline mm-trp__icon'/>
                <span className='mm-trp__label'>{value.label}</span>
                <i className={`icon mm-trp__chevron ${open ? 'icon-chevron-up' : 'icon-chevron-down'}`}/>
            </button>

            {open && (
                <div
                    className='mm-trp__popover'
                    role='dialog'
                    aria-label='Time range picker'
                >
                    <div className='mm-trp__absolute'>
                        <div className='mm-trp__abs-title'>{'Absolute time range'}</div>

                        <label className='mm-trp__abs-label'>{'From'}</label>
                        <input
                            type='datetime-local'
                            className='form-control mm-trp__datetime'
                            value={fromVal}
                            onChange={(e) => {
                                setFromVal(e.target.value);
                                setApplyError('');
                            }}
                        />

                        <label className='mm-trp__abs-label'>{'To'}</label>
                        <input
                            type='datetime-local'
                            className='form-control mm-trp__datetime'
                            value={toVal}
                            onChange={(e) => {
                                setToVal(e.target.value);
                                setApplyError('');
                            }}
                        />

                        {applyError && (
                            <div className='mm-trp__abs-error'>{applyError}</div>
                        )}

                        <button
                            type='button'
                            className='btn btn-primary mm-trp__apply'
                            onClick={handleApply}
                        >
                            {'Apply time range'}
                        </button>
                    </div>

                    <div className='mm-trp__quick'>
                        <input
                            type='text'
                            className='form-control mm-trp__search'
                            placeholder='Search quick ranges'
                            value={search}
                            onChange={(e) => setSearch(e.target.value)}
                            autoFocus={true}
                        />
                        <div className='mm-trp__quick-list'>
                            {filtered.map((qr) => (
                                <button
                                    key={qr.label}
                                    type='button'
                                    className={`mm-trp__quick-item${value.relative && value.label === qr.label ? ' mm-trp__quick-item--active' : ''}`}
                                    onClick={() => handleQuickRange(qr)}
                                >
                                    {qr.label}
                                </button>
                            ))}
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}
