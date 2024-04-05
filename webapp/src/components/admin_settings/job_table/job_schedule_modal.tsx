import React, {useState} from 'react';

import {Modal} from 'react-bootstrap';
import {addDays} from 'date-fns';
import {DateRange, DayPicker} from 'react-day-picker';

import 'react-day-picker/dist/style.css';

export type Props = {
    show: boolean;
    min_t: number | undefined;
    max_t: number | undefined;
    onClose: () => void;
    onSubmit: (range: DateRange | undefined) => void;
}

const styles = {
    buttonRow: {
        margin: '24px 24px',
    },
    btn: {
        gap: '0px',
    },
};

const JobScheduleModal = ({show, min_t, max_t, onClose, onSubmit}: Props) => {
    const today = new Date();
    let defaultSelected: DateRange = {
        from: addDays(today, -3),
        to: today,
    };

    let fromDate = addDays(today, -14);
    let toDate = today;
    if (min_t && max_t) {
        fromDate = new Date(min_t);
        toDate = new Date(max_t);

        const oneDay = 24 * 60 * 60 * 1000; // hours*minutes*seconds*milliseconds
        if (Math.round(Math.abs((max_t - min_t) / oneDay)) <= 3) {
            defaultSelected = {
                from: fromDate,
                to: toDate,
            };
        }
    }

    const [range, setRange] = useState<DateRange | undefined>(defaultSelected);
    return (
        <Modal
            show={show}
            onHide={onClose}
        >
            <Modal.Header closeButton={true}>
                <Modal.Title>
                    {'Range of the data to export'}
                </Modal.Title>
            </Modal.Header>
            <Modal.Body>
                <div>
                    <DayPicker
                        id='test'
                        mode='range'
                        defaultMonth={today}
                        fromDate={fromDate}
                        toDate={toDate}
                        selected={range}
                        onSelect={setRange}
                    />
                    <div
                        className='col-sm-13'
                        style={styles.buttonRow}
                    >
                        <a
                            className='btn btn-primary'
                            onClick={() => onSubmit(range)}
                        >
                            {'Submit'}
                        </a>
                    </div>
                </div>
            </Modal.Body>
        </Modal>
    );
};

export default JobScheduleModal;
