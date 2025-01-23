// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

import React from 'react';
import classNames from 'classnames';

import {DateRange} from 'react-day-picker';

import {Job, TSDBStats} from '../types/types';

import {createJob, deleteAllJobs, deleteJob, downloadJob, getJobs, getTSDBStats} from '../actions/actions';

import DateTimeFormatter from '../utils/date_time';

import JobDownloadLink from './job_download_link';
import JobScheduleModal from './job_schedule_modal';
import JobRemoveModal from './job_remove_modal';
import './job_schedule_modal.scss';

export type Props = {
    stats?: TSDBStats
    jobs: Job[];
    showScheduleModal: boolean;
    showRemoveModal: boolean;
    className?: string;
}

type State = {
    stats?: TSDBStats
    jobs: Job[];
    showScheduleModal: boolean;
    showRemoveModal: boolean;
    className?: string;
}

export default class JobTable extends React.Component<State, Props> {
    static defaultProps = {
        jobs: [],
        showScheduleModal: false,
        showRemoveModal: false,
    };

    constructor(props: Props) {
        super(props);
        this.state = {jobs: [], showScheduleModal: false, showRemoveModal: false};
    }

    interval: ReturnType<typeof setInterval>|null = null;

    async componentDidMount() {
        const jobs = await getJobs();
        const stats = await getTSDBStats();

        // eslint-disable-next-line react/no-did-mount-set-state
        this.setState({jobs, stats});
        this.interval = setInterval(this.reload, 15000);
    }

    componentWillUnmount() {
        if (this.interval) {
            clearInterval(this.interval);
        }
    }

    reload = async () => {
        const jobs = await getJobs();
        this.setState({jobs});
    };

    render() {
        const createDump = (range: DateRange) => {
            if (range.to) {
                // we need to manipulate one more day to the upper limit because the DayPicker
                // returns the 12:00 AM timestamp of the selected range.
                range.to = new Date(range.to!.getTime() + (1000 * 60 * 60 * 24));
            } else {
                // here we are setting upper limit if it's undefined, this happens if you
                // only select a single day.
                range.to = new Date(range.from!.getTime() + (1000 * 60 * 60 * 24));
            }

            createJob(range).finally(() => {
                this.reload();
                this.setState({showScheduleModal: false});
            });
        };

        const downloadDump = async (id: string) => {
            await downloadJob(id);
        };

        const removeJob = (id: string) => {
            deleteJob(id).finally(() => {
                this.reload();
            });
        };

        const deleteAll = () => {
            deleteAllJobs().finally(() => {
                this.reload();
                this.setState({showRemoveModal: false});
            });
        };

        const removeButton = () => {
            if (this.state.jobs.length > 0) {
                return (
                    <a
                        className={'btn btn-danger'}
                        onClick={() =>
                            this.setState({showRemoveModal: true})
                        }
                    >
                        {'Remove All'}
                    </a>
                );
            }

            return (
                <span
                    className={'btn btn-inactive'}
                >
                    {'Remove All'}
                </span>
            );
        };

        const items = this.state.jobs.map((job) => {
            return (
                <tr key={job.id} >
                    <td className='whitespace--nowrap'><DateTimeFormatter millis={job.create_at}/></td>
                    <td className='whitespace--nowrap'><DateTimeFormatter millis={job.min_t}/></td>
                    <td className='whitespace--nowrap'><DateTimeFormatter millis={job.max_t}/></td>
                    <td className='whitespace--nowrap'>
                        <JobDownloadLink
                            job={job}
                            download={downloadDump}
                            remove={removeJob}
                        />
                    </td>
                </tr>
            );
        });

        return (
            <div className={classNames('JobTable', 'job-table__panel', this.props.className)}>
                <div
                    className='col-sm-13'
                >
                    <a
                        className='btn btn-primary'
                        onClick={() =>
                            this.setState({showScheduleModal: true})
                        }
                    >
                        {'Create Dump'}
                    </a>
                    <JobScheduleModal
                        show={this.state.showScheduleModal}
                        min_t={this.state.stats?.min_t}
                        max_t={this.state.stats?.max_t}
                        onClose={() => this.setState({showScheduleModal: false})}
                        onSubmit={createDump}
                    />
                    {removeButton()}
                    <JobRemoveModal
                        show={this.state.showRemoveModal}
                        onClose={() => this.setState({showRemoveModal: false})}
                        onSubmit={deleteAll}
                    />
                </div>
                {
                    <div className='job-table__table'>
                        <table
                            className='table'
                            data-testid='jobTable'
                        >
                            <thead>
                                <tr>
                                    <th>
                                        {'Created At'}
                                    </th>
                                    <th>
                                        {'Minimum Time'}
                                    </th>
                                    <th>
                                        {'Maximum Time'}
                                    </th>
                                    <th>
                                        {'Actions'}
                                    </th>
                                </tr>
                            </thead>
                            <tbody>
                                {items}
                            </tbody>
                        </table>
                    </div>
                }
            </div>
        );
    }
}
