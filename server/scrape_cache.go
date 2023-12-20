package main

import (
	"fmt"
	"sync"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/exemplar"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/metadata"
	"github.com/prometheus/prometheus/model/textparse"
	"github.com/prometheus/prometheus/storage"
)

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

	metaMtx  sync.RWMutex
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
	defer c.metaMtx.Unlock()

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
}

func (c *scrapeCache) setHelp(metric, help []byte) {
	c.metaMtx.Lock()
	defer c.metaMtx.Unlock()

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
}

func (c *scrapeCache) setUnit(metric, unit []byte) {
	c.metaMtx.Lock()
	defer c.metaMtx.Unlock()

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
		level.Debug(logger).Log("msg", "Out of order sample", "series", string(met))
		return false, nil
	case storage.ErrDuplicateSampleForTimestamp:
		appErrs.numDuplicates++
		level.Debug(logger).Log("msg", "Duplicate sample for timestamp", "series", string(met))
		return false, nil
	case storage.ErrOutOfBounds:
		appErrs.numOutOfBounds++
		level.Debug(logger).Log("msg", "Out of bounds metric", "series", string(met))
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
		level.Debug(logger).Log("msg", "Out of order exemplar", "exemplar", fmt.Sprintf("%+v", e))
		return nil
	default:
		return err
	}
}
