package main

import (
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/storage"
)

var (
	errSampleLimit = errors.New("sample limit exceeded")
	errBucketLimit = errors.New("histogram bucket limit exceeded")
)

// appender returns an appender for ingested samples and sets the limits (if any provided)
func appender(app storage.Appender, sampleLimit, bucketLimit int) storage.Appender {
	// The sampleLimit is applied after metrics are potentially dropped via relabeling.
	if sampleLimit > 0 {
		app = &limitAppender{
			Appender: app,
			limit:    sampleLimit,
		}
	}

	if bucketLimit > 0 {
		app = &bucketLimitAppender{
			Appender: app,
			limit:    bucketLimit,
		}
	}
	return app
}

// limitAppender limits the number of total appended samples in a batch.
type limitAppender struct {
	storage.Appender

	limit int
	count int
}

func (app *limitAppender) Append(ref storage.SeriesRef, lset labels.Labels, t int64, v float64) (storage.SeriesRef, error) {
	if !value.IsStaleNaN(v) {
		app.count++
		if app.count > app.limit {
			return 0, errSampleLimit
		}
	}
	ref, err := app.Appender.Append(ref, lset, t, v)
	if err != nil {
		return 0, err
	}
	return ref, nil
}

// bucketLimitAppender limits the number of total appended samples in a batch.
type bucketLimitAppender struct {
	storage.Appender

	limit int
}

func (app *bucketLimitAppender) AppendHistogram(ref storage.SeriesRef, lset labels.Labels, t int64, h *histogram.Histogram, fh *histogram.FloatHistogram) (storage.SeriesRef, error) {
	if h != nil {
		if len(h.PositiveBuckets)+len(h.NegativeBuckets) > app.limit {
			return 0, errBucketLimit
		}
	}
	if fh != nil {
		if len(fh.PositiveBuckets)+len(fh.NegativeBuckets) > app.limit {
			return 0, errBucketLimit
		}
	}
	ref, err := app.Appender.AppendHistogram(ref, lset, t, h, fh)
	if err != nil {
		return 0, err
	}
	return ref, nil
}
