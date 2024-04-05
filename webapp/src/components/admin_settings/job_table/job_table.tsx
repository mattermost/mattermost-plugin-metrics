import React from 'react';
import classNames from 'classnames';
import marked from 'marked';

import {DateRange} from 'react-day-picker';

import {Job, TSDBStats} from '../types/types';

import {craeteJob, getJobs, getTSDBStats} from '../actions/actions';

import JobDateTime from './job_date_time';
import JobDownloadLink from './job_download_link';
import JobScheduleModal from './job_schedule_modal';
import './job_schedule_modal.scss';

export type Props = {
    stats?: TSDBStats
    jobs: Job[];
    showModal: boolean;
    className?: string;
}

type State = {
    stats?: TSDBStats
    jobs: Job[];
    showModal: boolean;
    className?: string;
}

export default class JobTable extends React.Component<State, Props> {
    static defaultProps = {
        jobs: [],
        showModal: false,
    };

    constructor(props: Props) {
        super(props);
        this.state = {jobs: [], showModal: false};
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
        const createDump = async (range: DateRange | undefined) => {
            await craeteJob(range!);

            this.setState({showModal: false});
            this.reload();
        };

        const items = this.state.jobs.map((job) => {
            return (
                <tr key={job.id} >
                    <td className='whitespace--nowrap'><JobDateTime millis={job.create_at}/></td>
                    <td className='whitespace--nowrap'><JobDateTime millis={job.min_t}/></td>
                    <td className='whitespace--nowrap'><JobDateTime millis={job.max_t}/></td>
                    <td className='whitespace--nowrap'>
                        <JobDownloadLink
                            job={job}
                            cb={this.reload}
                        />
                    </td>
                </tr>
            );
        });

        return (
            <div className={classNames('JobTable', 'job-table__panel', this.props.className)}>
                <div className='form-group'>
                    <label className='control-label col-sm-4'>
                        {'TSDB Stats:'}
                    </label>
                    <div className='col-sm-8'>
                        <span>
                            {`Number of samples: ${this.state.stats?.num_samples},\nNumber of series: ${this.state.stats?.num_series}`}
                        </span>
                    </div>
                </div>
                <div
                    className='col-sm-13'
                >
                    <a
                        className='btn btn-primary'
                        onClick={() =>
                            this.setState({showModal: true})
                        }
                    >
                        {'Create Dump'}
                    </a>
                    <JobScheduleModal
                        show={this.state.showModal}
                        min_t={this.state.stats?.min_t}
                        max_t={this.state.stats?.max_t}
                        onClose={() => this.setState({showModal: false})}
                        onSubmit={createDump}
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
                                        {'Min T'}
                                    </th>
                                    <th>
                                        {'Max T'}
                                    </th>
                                    <th>
                                        {'Action(s)'}
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
