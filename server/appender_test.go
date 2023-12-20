package main

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/mattermost/mattermost-plugin-metrics/server/mocks"
)

func nopMutator(l labels.Labels) labels.Labels { return l }

func TestAppendFn(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ma := mocks.NewMockAppender(ctrl)
	ml := mocks.NewMockLogger(ctrl)
	lg := &metricsLogger{api: ml}

	cf := &configuration{}
	cf.SetDefaults()

	sampleMutator = nopMutator

	t.Run("empty body", func(t *testing.T) {
		ch := newScrapeCache()
		err := appendFn(ma, lg, ch, []byte{}, "application/openmetrics-text", time.Now(), cf)
		require.EqualError(t, err, "data does not end with # EOF")
	})

	t.Run("metric group", func(t *testing.T) {
		ch := newScrapeCache()
		body := `# TYPE foo_seconds counter
# UNIT foo_seconds seconds
foo_seconds_count 0
foo_seconds_sum 1
# EOF
`
		ts := time.Now()
		ls := labels.Labels{{Name: "__name__", Value: "foo_seconds_count"}}
		ma.EXPECT().Append(storage.SeriesRef(0), ls, ts.UnixMilli(), float64(0)).Times(1)
		ls = labels.Labels{{Name: "__name__", Value: "foo_seconds_sum"}}
		ma.EXPECT().Append(storage.SeriesRef(0), ls, ts.UnixMilli(), float64(1)).Times(1)
		err := appendFn(ma, lg, ch, []byte(body), "application/openmetrics-text", ts, cf)
		require.NoError(t, err)
	})

	t.Run("metric histogram", func(t *testing.T) {
		ch := newScrapeCache()
		body := `# TYPE foo histogram
foo_bucket{le="0.0"} 0
foo_bucket{le="0.1"} 1
# EOF
`
		ts := time.Now()
		ls := labels.Labels{
			{Name: "__name__", Value: "foo_bucket"},
			{Name: "le", Value: "0.0"},
		}
		ma.EXPECT().Append(storage.SeriesRef(0), ls, ts.UnixMilli(), float64(0)).Times(1)
		ls = labels.Labels{
			{Name: "__name__", Value: "foo_bucket"},
			{Name: "le", Value: "0.1"},
		}
		ma.EXPECT().Append(storage.SeriesRef(0), ls, ts.UnixMilli(), float64(1)).Times(1)
		err := appendFn(ma, lg, ch, []byte(body), "application/openmetrics-text", ts, cf)
		require.NoError(t, err)
	})

	t.Run("metric type unknown", func(t *testing.T) {
		ch := newScrapeCache()
		body := `# TYPE foo unknown
foo 42.23
# EOF
`
		ts := time.Now()
		ls := labels.Labels{{Name: "__name__", Value: "foo"}}
		ma.EXPECT().Append(storage.SeriesRef(0), ls, ts.UnixMilli(), 42.23).Times(1)
		ma.EXPECT().UpdateMetadata(storage.SeriesRef(0), ls, gomock.Any()).Times(1)
		err := appendFn(ma, lg, ch, []byte(body), "application/openmetrics-text", ts, cf)
		require.NoError(t, err)
	})

	t.Run("sample limit", func(t *testing.T) {
		ch := newScrapeCache()
		body := `# TYPE foo_count gauge
foo_count 0 123
foo_count 1 124
# EOF
`
		defer func() {
			*cf.SampleLimit = 1000
		}()
		*cf.SampleLimit = 1
		ts := time.Now()
		ls := labels.Labels{{Name: "__name__", Value: "foo_count"}}
		ma.EXPECT().Append(storage.SeriesRef(0), ls, int64(123000), float64(0)).Times(1)
		ma.EXPECT().UpdateMetadata(storage.SeriesRef(0), ls, gomock.Any()).Times(2)
		err := appendFn(ma, lg, ch, []byte(body), "application/openmetrics-text", ts, cf)
		require.EqualError(t, err, "sample limit exceeded")
	})
}
