import React, {useState} from 'react';

import {Modal} from 'react-bootstrap';
import {addDays} from 'date-fns';
import {DateRange, DayPicker} from 'react-day-picker';

import 'react-day-picker/dist/style.css';

export type Props = {
    show: boolean;
    onClose: () => void;
    onSubmit: (range: DateRange) => void;
}

const styles = {
    buttonRow: {
        margin: '24px 24px',
    },
    btn: {
        gap: '0px',
    },
};

const JobScheduleModal = ({show, onClose, onSubmit}: Props) => {
    const today = new Date();
    const defaultSelected: DateRange = {
        from: addDays(today, -3),
        to: today,
    };
    const [range, setRange] = useState<DateRange | undefined>(defaultSelected);

    return (
        <Modal
            dialogClassName='a11y__modal metrics-modal-schedule'
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
                        fromDate={addDays(today, -14)}
                        toDate={today}
                        selected={range}
                        onSelect={setRange}
                    />
                    <div
                        className='col-sm-13'
                        style={styles.buttonRow}
                    >
                        <a
                            className='btn btn-primary'
                            onClick={() => onSubmit(range!)}
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
