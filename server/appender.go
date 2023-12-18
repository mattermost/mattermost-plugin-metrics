package main

import (
	"fmt"
	"io"
	"math"
	"sync"
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
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/storage"
)

var errNameLabelMandatory = fmt.Errorf("missing metric name (%s label)", labels.MetricName)

type appendErrors struct {
	numOutOfOrder         int
	numDuplicates         int
	numOutOfBounds        int
	numExemplarOutOfOrder int
}

// appendFn an scrapecache are copied from prometheus project. Ideally it should've been used from the public API.
// The public API allows using the ScrapeManager only. We can later use scrape.ScrapeManager but for
// now we are only using the bits necessary for the project.
func appendFn(app storage.Appender, logger log.Logger, cache *scrapeCache, b []byte, contentType string, ts time.Time, conf *configuration) error {
	p, err := textparse.New(b, contentType, true)
	if err != nil {
		_ = level.Debug(logger).Log(
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

		cache.metaMtx.Lock()
		defer cache.metaMtx.Unlock()
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
	app = appender(app, *conf.SampleLimit, *conf.BucketLimit)

	defer func() {
		if err != nil {
			return
		}
		// Only perform cache cleaning if the scrape was not empty.
		// An empty scrape (usually) is used to indicate a failed scrape.
		cache.iterDone(len(b) > 0)
	}()

loop:
	for {
		var (
			et              textparse.Entry
			isHistogram     bool
			met             []byte
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
			met, parsedTimestamp, h, fh = p.Histogram()
		} else {
			met, parsedTimestamp, val = p.Series()
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

		if cache.getDropped(met) {
			continue
		}
		ce, ok := cache.get(met)
		var (
			ref  storage.SeriesRef
			hash uint64
		)

		if ok {
			ref = ce.ref
			lset = ce.lset

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
				cache.addDropped(met)
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
				ref, err = app.AppendHistogram(ref, lset, t, h, nil)
			} else {
				ref, err = app.AppendHistogram(ref, lset, t, nil, fh)
			}
		} else {
			ref, err = app.Append(ref, lset, t, val)
		}
		_, err = checkAddError(cache, ce, logger, met, parsedTimestamp, err, &sampleLimitErr, &bucketLimitErr, &appErrs)
		if err != nil {
			if err != storage.ErrNotFound {
				_ = level.Debug(logger).Log("msg", "Unexpected error", "series", string(met), "err", err)
			}
			break loop
		}

		if !ok {
			if parsedTimestamp == nil {
				// Bypass staleness logic if there is an explicit timestamp.
				cache.trackStaleness(hash, lset)
			}
			cache.addRef(met, ref, lset, hash)
		}

		for hasExemplar := p.Exemplar(&e); hasExemplar; hasExemplar = p.Exemplar(&e) {
			if !e.HasTs {
				e.Ts = t
			}
			_, exemplarErr := app.AppendExemplar(ref, lset, e)
			exemplarErr = checkAddExemplarError(logger, exemplarErr, e, &appErrs)
			if exemplarErr != nil {
				// Since exemplar storage is still experimental, we don't fail the scrape on ingestion errors.
				_ = level.Debug(logger).Log("msg", "Error while adding exemplar in AddExemplar", "exemplar", fmt.Sprintf("%+v", e), "err", exemplarErr)
			}
			e = exemplar.Exemplar{} // reset for next time round loop
		}

		if *conf.EnableMetadataStorage && metadataChanged {
			if _, merr := app.UpdateMetadata(ref, lset, meta); merr != nil {
				// No need to fail the scrape on errors appending metadata.
				_ = level.Debug(logger).Log("msg", "Error when appending metadata in scrape loop", "ref", fmt.Sprintf("%d", ref), "metadata", fmt.Sprintf("%+v", meta), "err", merr)
			}
		}
	}
	if sampleLimitErr != nil {
		if err == nil {
			err = sampleLimitErr
		}
	}
	if bucketLimitErr != nil {
		if err == nil {
			err = bucketLimitErr // If sample limit is hit, that error takes precedence.
		}
	}
	if appErrs.numOutOfOrder > 0 {
		_ = level.Warn(logger).Log("msg", "Error on ingesting out-of-order samples", "num_dropped", appErrs.numOutOfOrder)
	}
	if appErrs.numDuplicates > 0 {
		_ = level.Warn(logger).Log("msg", "Error on ingesting samples with different value but same timestamp", "num_dropped", appErrs.numDuplicates)
	}
	if appErrs.numOutOfBounds > 0 {
		_ = level.Warn(logger).Log("msg", "Error on ingesting samples that are too old or are too far into the future", "num_dropped", appErrs.numOutOfBounds)
	}
	if appErrs.numExemplarOutOfOrder > 0 {
		_ = level.Warn(logger).Log("msg", "Error on ingesting out-of-order exemplars", "num_dropped", appErrs.numExemplarOutOfOrder)
	}
	if err == nil {
		cache.forEachStale(func(lset labels.Labels) bool {
			// Series no longer exposed, mark it stale.
			_, err = app.Append(0, lset, defTime, math.Float64frombits(value.StaleNaN))
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

type cacheEntry struct {
	ref      storage.SeriesRef
	lastIter uint64
	hash     uint64
	lset     labels.Labels
}

// metaEntry holds meta information about a metric.
type metaEntry struct {
	metadata.Metadata

	lastIter       uint64 // Last scrape iteration the entry was observed at.
	lastIterChange uint64 // Last scrape iteration the entry was changed at.
}

// scrapeCache tracks mappings of exposed metric strings to label sets and
// storage references. Additionally, it tracks staleness of series between
// scrapes.
type scrapeCache struct {
	iter uint64 // Current scrape iteration.

	// How many series and metadata entries there were at the last success.
	successfulCount int

	// Parsed string to an entry with information about the actual label set
	// and its storage reference.
	series map[string]*cacheEntry

	// Cache of dropped metric strings and their iteration. The iteration must
	// be a pointer so we can update it.
	droppedSeries map[string]*uint64

	// seriesCur and seriesPrev store the labels of series that were seen
	// in the current and previous scrape.
	// We hold two maps and swap them out to save allocations.
	seriesCur  map[uint64]labels.Labels
	seriesPrev map[uint64]labels.Labels

	metaMtx  sync.Mutex
	metadata map[string]*metaEntry
}

func newScrapeCache() *scrapeCache {
	return &scrapeCache{
		series:        map[string]*cacheEntry{},
		droppedSeries: map[string]*uint64{},
		seriesCur:     map[uint64]labels.Labels{},
		seriesPrev:    map[uint64]labels.Labels{},
		metadata:      map[string]*metaEntry{},
	}
}

func (c *scrapeCache) iterDone(flushCache bool) {
	c.metaMtx.Lock()
	count := len(c.series) + len(c.droppedSeries) + len(c.metadata)
	c.metaMtx.Unlock()

	switch {
	case flushCache:
		c.successfulCount = count
	case count > c.successfulCount*2+1000:
		// If a target had varying labels in scrapes that ultimately failed,
		// the caches would grow indefinitely. Force a flush when this happens.
		// We use the heuristic that this is a doubling of the cache size
		// since the last scrape, and allow an additional 1000 in case
		// initial scrapes all fail.
		flushCache = true
	}

	if flushCache {
		// All caches may grow over time through series churn
		// or multiple string representations of the same metric. Clean up entries
		// that haven't appeared in the last scrape.
		for s, e := range c.series {
			if c.iter != e.lastIter {
				delete(c.series, s)
			}
		}
		for s, iter := range c.droppedSeries {
			if c.iter != *iter {
				delete(c.droppedSeries, s)
			}
		}
		c.metaMtx.Lock()
		for m, e := range c.metadata {
			// Keep metadata around for 10 scrapes after its metric disappeared.
			if c.iter-e.lastIter > 10 {
				delete(c.metadata, m)
			}
		}
		c.metaMtx.Unlock()

		c.iter++
	}

	// Swap current and previous series.
	c.seriesPrev, c.seriesCur = c.seriesCur, c.seriesPrev

	// We have to delete every single key in the map.
	for k := range c.seriesCur {
		delete(c.seriesCur, k)
	}
}

func (c *scrapeCache) get(met []byte) (*cacheEntry, bool) {
	e, ok := c.series[string(met)]
	if !ok {
		return nil, false
	}
	e.lastIter = c.iter
	return e, true
}

func (c *scrapeCache) addRef(met []byte, ref storage.SeriesRef, lset labels.Labels, hash uint64) {
	if ref == 0 {
		return
	}
	c.series[string(met)] = &cacheEntry{ref: ref, lastIter: c.iter, lset: lset, hash: hash}
}

func (c *scrapeCache) addDropped(met []byte) {
	iter := c.iter
	c.droppedSeries[string(met)] = &iter
}

func (c *scrapeCache) getDropped(met []byte) bool {
	iterp, ok := c.droppedSeries[string(met)]
	if ok {
		*iterp = c.iter
	}
	return ok
}

func (c *scrapeCache) trackStaleness(hash uint64, lset labels.Labels) {
	c.seriesCur[hash] = lset
}

func (c *scrapeCache) forEachStale(f func(labels.Labels) bool) {
	for h, lset := range c.seriesPrev {
		if _, ok := c.seriesCur[h]; !ok {
			if !f(lset) {
				break
			}
		}
	}
}

func (c *scrapeCache) setType(metric []byte, t textparse.MetricType) {
	c.metaMtx.Lock()

	e, ok := c.metadata[string(metric)]
	if !ok {
		e = &metaEntry{Metadata: metadata.Metadata{Type: textparse.MetricTypeUnknown}}
		c.metadata[string(metric)] = e
	}
	if e.Type != t {
		e.Type = t
		e.lastIterChange = c.iter
	}
	e.lastIter = c.iter

	c.metaMtx.Unlock()
}

func (c *scrapeCache) setHelp(metric, help []byte) {
	c.metaMtx.Lock()

	e, ok := c.metadata[string(metric)]
	if !ok {
		e = &metaEntry{Metadata: metadata.Metadata{Type: textparse.MetricTypeUnknown}}
		c.metadata[string(metric)] = e
	}
	if e.Help != string(help) {
		e.Help = string(help)
		e.lastIterChange = c.iter
	}
	e.lastIter = c.iter

	c.metaMtx.Unlock()
}

func (c *scrapeCache) setUnit(metric, unit []byte) {
	c.metaMtx.Lock()

	e, ok := c.metadata[string(metric)]
	if !ok {
		e = &metaEntry{Metadata: metadata.Metadata{Type: textparse.MetricTypeUnknown}}
		c.metadata[string(metric)] = e
	}
	if e.Unit != string(unit) {
		e.Unit = string(unit)
		e.lastIterChange = c.iter
	}
	e.lastIter = c.iter

	c.metaMtx.Unlock()
}

func (c *scrapeCache) GetMetadata(metric string) (scrape.MetricMetadata, bool) {
	c.metaMtx.Lock()
	defer c.metaMtx.Unlock()

	m, ok := c.metadata[metric]
	if !ok {
		return scrape.MetricMetadata{}, false
	}
	return scrape.MetricMetadata{
		Metric: metric,
		Type:   m.Type,
		Help:   m.Help,
		Unit:   m.Unit,
	}, true
}

func (c *scrapeCache) ListMetadata() []scrape.MetricMetadata {
	c.metaMtx.Lock()
	defer c.metaMtx.Unlock()

	res := make([]scrape.MetricMetadata, 0, len(c.metadata))

	for m, e := range c.metadata {
		res = append(res, scrape.MetricMetadata{
			Metric: m,
			Type:   e.Type,
			Help:   e.Help,
			Unit:   e.Unit,
		})
	}
	return res
}

// MetadataSize returns the size of the metadata cache.
func (c *scrapeCache) SizeMetadata() (s int) {
	c.metaMtx.Lock()
	defer c.metaMtx.Unlock()
	for _, e := range c.metadata {
		s += e.size()
	}

	return s
}

// MetadataLen returns the number of metadata entries in the cache.
func (c *scrapeCache) LengthMetadata() int {
	c.metaMtx.Lock()
	defer c.metaMtx.Unlock()

	return len(c.metadata)
}

func (m *metaEntry) size() int {
	// The attribute lastIter although part of the struct it is not metadata.
	return len(m.Help) + len(m.Unit) + len(m.Type)
}

// Adds samples to the appender, checking the error, and then returns the # of samples added,
// whether the caller should continue to process more samples, and any sample or bucket limit errors.
func checkAddError(cache *scrapeCache, ce *cacheEntry, logger log.Logger, met []byte, tp *int64, err error, sampleLimitErr, bucketLimitErr *error, appErrs *appendErrors) (bool, error) {
	switch errors.Cause(err) {
	case nil:
		if tp == nil && ce != nil {
			cache.trackStaleness(ce.hash, ce.lset)
		}
		return true, nil
	case storage.ErrNotFound:
		return false, storage.ErrNotFound
	case storage.ErrOutOfOrderSample:
		appErrs.numOutOfOrder++
		_ = level.Debug(logger).Log("msg", "Out of order sample", "series", string(met))
		return false, nil
	case storage.ErrDuplicateSampleForTimestamp:
		appErrs.numDuplicates++
		_ = level.Debug(logger).Log("msg", "Duplicate sample for timestamp", "series", string(met))
		return false, nil
	case storage.ErrOutOfBounds:
		appErrs.numOutOfBounds++
		_ = level.Debug(logger).Log("msg", "Out of bounds metric", "series", string(met))
		return false, nil
	case errSampleLimit:
		// Keep on parsing output if we hit the limit, so we report the correct
		// total number of samples scraped.
		*sampleLimitErr = err
		return false, nil
	case errBucketLimit:
		// Keep on parsing output if we hit the limit, so we report the correct
		// total number of samples scraped.
		*bucketLimitErr = err
		return false, nil
	default:
		return false, err
	}
}

func checkAddExemplarError(logger log.Logger, err error, e exemplar.Exemplar, appErrs *appendErrors) error {
	switch errors.Cause(err) {
	case storage.ErrNotFound:
		return storage.ErrNotFound
	case storage.ErrOutOfOrderExemplar:
		appErrs.numExemplarOutOfOrder++
		_ = level.Debug(logger).Log("msg", "Out of order exemplar", "exemplar", fmt.Sprintf("%+v", e))
		return nil
	default:
		return err
	}
}
