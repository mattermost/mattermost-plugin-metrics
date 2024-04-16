import React from 'react';

import DateTimeFormatter from '../utils/date_time';
import {getTSDBStats} from '../actions/actions';
import {TSDBStats} from '../types/types';

import './tsdb_stats.scss';

export type Props = {}

type State = {
    stats: TSDBStats
}

class TSDBStatsTable extends React.PureComponent<Props, State> {
    constructor(props: Props) {
        super(props);
        this.state = {
            stats: {
                min_t: 0,
                max_t: 0,
                num_series: 0,
                num_samples: 0,
            },
        };
    }

    async componentDidMount() {
        const stats = await getTSDBStats();

        // eslint-disable-next-line react/no-did-mount-set-state
        this.setState({stats});
    }

    render() {
        return (
            <div className='form-group'>
                <label className='control-label col-sm-4'>
                    {'TSDB Stats:'}
                </label>
                <div className='tsdbstats-table col-sm-8'>
                    <div className='tsdbstats-table__table'>
                        <table
                            className='table'
                        >
                            <thead>
                                <tr>
                                    <th>
                                        {'Name'}
                                    </th>
                                    <th>
                                        {'Value'}
                                    </th>
                                </tr>
                            </thead>
                            <tbody>
                                <tr
                                    key={'num_samples'}
                                >
                                    <td className='whitespace--nowrap'>{'Number of samples'}</td>
                                    <td className='whitespace--nowrap'>{this.state.stats.num_samples}</td>
                                </tr>
                                <tr
                                    key={'num_series'}
                                >
                                    <td className='whitespace--nowrap'>{'Maximum series count'}</td>
                                    <td className='whitespace--nowrap'>{this.state.stats.num_series}</td>
                                </tr>
                                <tr
                                    key={'oldest_sample'}
                                >
                                    <td className='whitespace--nowrap'>{'Oldest timestamp'}</td>
                                    <td className='whitespace--nowrap'><DateTimeFormatter millis={this.state.stats.min_t}/></td>
                                </tr>
                                <tr
                                    key={'youngest_sample'}
                                >
                                    <td className='whitespace--nowrap'>{'Most recent timestamp'}</td>
                                    <td className='whitespace--nowrap'><DateTimeFormatter millis={this.state.stats.max_t}/></td>
                                </tr>
                            </tbody>
                        </table>
                    </div>
                </div>
            </div>
        );
    }
}

export default TSDBStatsTable;
