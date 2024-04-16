import React from 'react';

import {Modal} from 'react-bootstrap';

import 'react-day-picker/dist/style.css';

export type Props = {
    show: boolean;
    onClose: () => void;
    onSubmit: () => void;
}

const styles = {
    buttonRow: {
        margin: '24px 0px',
    },
    btn: {
        gap: '0px',
    },
};

const JobRemoveModal = ({show, onClose, onSubmit}: Props) => {
    return (
        <Modal
            show={show}
            onHide={onClose}
        >
            <Modal.Header closeButton={true}>
                <Modal.Title>
                    {'Confirm removal'}
                </Modal.Title>
            </Modal.Header>
            <Modal.Body>
                <div>
                    <div className='col-sm-13'>
                        {'This will remove all jobs and delete the dump directory from the file store.'}
                    </div>
                    <div
                        className='col-sm-13'
                        style={styles.buttonRow}
                    >
                        <a
                            className='btn btn-primary'
                            onClick={() => onSubmit()}
                        >
                            {'Confirm'}
                        </a>
                    </div>
                </div>
            </Modal.Body>
        </Modal>
    );
};

export default JobRemoveModal;
