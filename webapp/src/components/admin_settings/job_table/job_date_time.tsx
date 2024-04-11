import React, {ReactElement} from 'react';
import {IntlProvider, FormattedTime, FormattedDate} from 'react-intl';

type Props = {
    millis: number;
}

const JobDateTime = React.memo(({millis}: Props): ReactElement => {
    const date = new Date(millis);

    return (
        <span className='JobFinishAt whitespace--nowrap'>
            <FormattedDate
                value={date}
                day='2-digit'
                month='short'
                year='numeric'
            />
            {' - '}
            <FormattedTime
                value={date}
                hour='2-digit'
                minute='2-digit'
            />
        </span>
    );
});

export default JobDateTime;
