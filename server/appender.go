package main

import (
	"fmt"
	"io"
	"math"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/exemplar"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/metadata"
	"github.com/prometheus/prometheus/model/textparse"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/storage"
)

var errNameLabelMandatory = fmt.Errorf("missing metric name (%s label)", labels.MetricName)

type appendErrors struct {
	numOutOfOrder         int
	numDuplicates         int
	numOutOfBounds        int
	numExemplarOutOfOrder int
}

// appendFn and scrapecache are copied from prometheus project. Ideally it should've been used from the public API.
// The public API allows using the ScrapeManager only. We can later use scrape.ScrapeManager but for
// now we are only using the bits necessary for the project.
func appendFn(app storage.Appender, logger log.Logger, cache *scrapeCache, b []byte, contentType string, ts time.Time, conf *configuration) error {
	// textparse.New always returns a valid parser even if there is an error. In that case
	// it will return the default parser
	p, err := textparse.New(b, contentType, true)
	if err != nil {
		level.Debug(logger).Log(
			"msg", "Invalid content type on scrape, using prometheus parser as fallback.",
			"content_type", contentType,
			"err", err,
		)
	}

	var (
		defTime         = timestamp.FromTime(ts)
		appErrs         = appendErrors{}
		sampleLimitErr  error
		bucketLimitErr  error
		lset            labels.Labels     // escapes to heap so hoisted out of loop
		e               exemplar.Exemplar // escapes to heap so hoisted out of loop
		meta            metadata.Metadata
		metadataChanged bool
	)

	// updateMetadata updates the current iteration's metadata object and the
	// metadataChanged value if we have metadata in the scrape cache AND the
	// labelset is for a new series or the metadata for this series has just
	// changed. It returns a boolean based on whether the metadata was updated.
	updateMetadata := func(lset labels.Labels, isNewSeries bool) bool {
		if !*conf.EnableMetadataStorage {
			return false
		}

		cache.metaMtx.RLock()
		defer cache.metaMtx.RUnlock()
		metaEntry, metaOk := cache.metadata[lset.Get(labels.MetricName)]
		if metaOk && (isNewSeries || metaEntry.lastIterChange == cache.iter) {
			metadataChanged = true
			meta.Type = metaEntry.Type
			meta.Unit = metaEntry.Unit
			meta.Help = metaEntry.Help
			return true
		}
		return false
	}

	// Take an appender with limits.
	appenderWithLimits := appender(app, *conf.SampleLimit, *conf.BucketLimit)

	defer func() {
		if err == nil {
			// Only perform cache cleaning if the scrape was not empty.
			// An empty scrape (usually) is used to indicate a failed scrape.
			cache.iterDone(len(b) > 0)
		}
	}()

loop:
	for {
		var (
			et              textparse.Entry
			isHistogram     bool
			metric          []byte
			parsedTimestamp *int64
			val             float64
			h               *histogram.Histogram
			fh              *histogram.FloatHistogram
		)
		if et, err = p.Next(); err != nil {
			if errors.Is(err, io.EOF) {
				err = nil
			}
			break
		}
		switch et {
		case textparse.EntryType:
			cache.setType(p.Type())
			continue
		case textparse.EntryHelp:
			cache.setHelp(p.Help())
			continue
		case textparse.EntryUnit:
			cache.setUnit(p.Unit())
			continue
		case textparse.EntryComment:
			continue
		case textparse.EntryHistogram:
			isHistogram = true
		default:
		}

		t := defTime
		if isHistogram {
			metric, parsedTimestamp, h, fh = p.Histogram()
		} else {
			metric, parsedTimestamp, val = p.Series()
		}
		if !*conf.HonorTimestamps {
			parsedTimestamp = nil
		}
		if parsedTimestamp != nil {
			t = *parsedTimestamp
		}

		// Zero metadata out for current iteration until it's resolved.
		meta = metadata.Metadata{}
		metadataChanged = false

		if cache.getDropped(metric) {
			continue
		}
		entry, ok := cache.get(metric)
		var (
			ref  storage.SeriesRef
			hash uint64
		)

		if ok {
			ref = entry.ref
			lset = entry.lset

			// Update metadata only if it changed in the current iteration.
			updateMetadata(lset, false)
		} else {
			p.Metric(&lset)
			hash = lset.Hash()

			// Hash label set as it is seen local to the target. Then add target labels
			// and relabeling and store the final label set.
			lset = sampleMutator(lset)

			// The label set may be set to empty to indicate dropping.
			if lset.IsEmpty() {
				cache.addDropped(metric)
				continue
			}

			if !lset.Has(labels.MetricName) {
				err = errNameLabelMandatory
				break loop
			}
			if !lset.IsValid() {
				err = fmt.Errorf("invalid metric name or label names: %s", lset.String())
				break loop
			}

			// Append metadata for new series if they were present.
			updateMetadata(lset, true)
		}

		if isHistogram {
			if h != nil {
				ref, err = appenderWithLimits.AppendHistogram(ref, lset, t, h, nil)
			} else {
				ref, err = appenderWithLimits.AppendHistogram(ref, lset, t, nil, fh)
			}
		} else {
			ref, err = appenderWithLimits.Append(ref, lset, t, val)
		}
		_, err = checkAddError(cache, entry, logger, metric, parsedTimestamp, err, &sampleLimitErr, &bucketLimitErr, &appErrs)
		if err != nil {
			if err != storage.ErrNotFound {
				level.Debug(logger).Log("msg", "Unexpected error", "series", string(metric), "err", err)
			}
			break loop
		}

		if !ok {
			if parsedTimestamp == nil {
				// Bypass staleness logic if there is an explicit timestamp.
				cache.trackStaleness(hash, lset)
			}
			cache.addRef(metric, ref, lset, hash)
		}

		for hasExemplar := p.Exemplar(&e); hasExemplar; hasExemplar = p.Exemplar(&e) {
			if !e.HasTs {
				e.Ts = t
			}
			_, exemplarErr := appenderWithLimits.AppendExemplar(ref, lset, e)
			exemplarErr = checkAddExemplarError(logger, exemplarErr, e, &appErrs)
			if exemplarErr != nil {
				// Since exemplar storage is still experimental, we don't fail the scrape on ingestion errors.
				level.Debug(logger).Log("msg", "Error while adding exemplar in AddExemplar", "exemplar", fmt.Sprintf("%+v", e), "err", exemplarErr)
			}
			e = exemplar.Exemplar{} // reset for next time round loop
		}

		if *conf.EnableMetadataStorage && metadataChanged {
			if _, merr := appenderWithLimits.UpdateMetadata(ref, lset, meta); merr != nil {
				// No need to fail the scrape on errors appending metadata.
				level.Debug(logger).Log("msg", "Error when appending metadata in scrape loop", "ref", fmt.Sprintf("%d", ref), "metadata", fmt.Sprintf("%+v", meta), "err", merr)
			}
		}
	}
	if sampleLimitErr != nil && err == nil {
		err = sampleLimitErr
	}

	if bucketLimitErr != nil && err == nil {
		err = bucketLimitErr // If sample limit is hit, that error takes precedence.
	}

	if appErrs.numOutOfOrder > 0 {
		level.Warn(logger).Log("msg", "Error on ingesting out-of-order samples", "num_dropped", appErrs.numOutOfOrder)
	}
	if appErrs.numDuplicates > 0 {
		level.Warn(logger).Log("msg", "Error on ingesting samples with different value but same timestamp", "num_dropped", appErrs.numDuplicates)
	}
	if appErrs.numOutOfBounds > 0 {
		level.Warn(logger).Log("msg", "Error on ingesting samples that are too old or are too far into the future", "num_dropped", appErrs.numOutOfBounds)
	}
	if appErrs.numExemplarOutOfOrder > 0 {
		level.Warn(logger).Log("msg", "Error on ingesting out-of-order exemplars", "num_dropped", appErrs.numExemplarOutOfOrder)
	}
	if err == nil {
		cache.forEachStale(func(lset labels.Labels) bool {
			// Series no longer exposed, mark it stale.
			_, err = appenderWithLimits.Append(0, lset, defTime, math.Float64frombits(value.StaleNaN))
			switch errors.Cause(err) {
			case storage.ErrOutOfOrderSample, storage.ErrDuplicateSampleForTimestamp:
				// Do not count these in logging, as this is expected if a target
				// goes away and comes back again with a new scrape loop.
				err = nil
			}
			return err == nil
		})
	}

	return err
}
