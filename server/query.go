// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/prometheus/prometheus/promql"
)

type QueryRequest struct {
	Query string  `json:"query"`
	Start int64   `json:"start"` // unix seconds
	End   int64   `json:"end"`   // unix seconds
	Step  float64 `json:"step"`  // seconds
}

type QuerySeries struct {
	Metric map[string]string `json:"metric"`
	Values [][2]float64      `json:"values"` // [unix_seconds, value]
}

type QueryResult struct {
	Query  string        `json:"query"`
	Series []QuerySeries `json:"series"`
	Error  string        `json:"error,omitempty"`
}

type BatchQueryRequest struct {
	Start   int64    `json:"start"` // unix seconds, shared across all queries
	End     int64    `json:"end"`   // unix seconds
	Step    float64  `json:"step"`  // seconds
	Queries []string `json:"queries"`
}

type BatchQueryResponse struct {
	Results               []QueryResult `json:"results"`
	DataStart             int64         `json:"data_start"` // unix seconds of the oldest queryable sample, 0 if none
	ScrapeIntervalSeconds int           `json:"scrape_interval_seconds"`
}

// DataStart returns the unix time in seconds of the oldest queryable sample, or 0 if there is none yet.
func (p *Plugin) DataStart() (int64, error) {
	p.tsdbLock.RLock()
	db := p.db
	p.tsdbLock.RUnlock()
	if db == nil {
		return 0, nil
	}

	startMs, err := db.StartTime()
	if err != nil {
		return 0, err
	}
	// An empty head reports math.MaxInt64 as its min time.
	if startMs == math.MaxInt64 {
		return 0, nil
	}
	return startMs / 1000, nil
}

func (p *Plugin) newQueryEngine() *promql.Engine {
	return promql.NewEngine(promql.EngineOpts{
		Logger:               p.logger,
		Reg:                  nil,
		MaxSamples:           50_000_000,
		Timeout:              30 * time.Second,
		LookbackDelta:        5 * time.Minute,
		EnableAtModifier:     true,
		EnableNegativeOffset: true,
	})
}

func (p *Plugin) QueryRange(ctx context.Context, req QueryRequest) QueryResult {
	// hold the lock for the whole query so that the tsdb is not closed under it
	p.tsdbLock.RLock()
	defer p.tsdbLock.RUnlock()
	db := p.db
	engine := p.queryEngine

	if engine == nil || db == nil {
		return QueryResult{Query: req.Query, Error: "query engine not available"}
	}

	const minStep = time.Minute

	start := time.Unix(req.Start, 0)
	end := time.Unix(req.End, 0)
	step := time.Duration(req.Step * float64(time.Second))
	if step < minStep {
		step = minStep
	}

	q, err := engine.NewRangeQuery(ctx, db, nil, req.Query, start, end, step)
	if err != nil {
		return QueryResult{Query: req.Query, Error: fmt.Sprintf("query error: %s", err.Error())}
	}
	defer q.Close()

	res := q.Exec(ctx)
	if res.Err != nil {
		return QueryResult{Query: req.Query, Error: res.Err.Error()}
	}

	matrix, ok := res.Value.(promql.Matrix)
	if !ok {
		return QueryResult{Query: req.Query, Error: "unexpected result type"}
	}

	series := make([]QuerySeries, 0, len(matrix))
	for _, s := range matrix {
		values := make([][2]float64, 0, len(s.Floats))
		for _, fp := range s.Floats {
			v := fp.F
			if math.IsNaN(v) || math.IsInf(v, 0) {
				v = 0
			}
			values = append(values, [2]float64{float64(fp.T) / 1000.0, v})
		}
		series = append(series, QuerySeries{
			Metric: s.Metric.Map(),
			Values: values,
		})
	}

	return QueryResult{Query: req.Query, Series: series}
}

// QueryBatch runs the queries against the local TSDB. In a cluster, only the scraping node
// has a TSDB, so the other nodes forward the queries to it.
func (p *Plugin) QueryBatch(ctx context.Context, req BatchQueryRequest) (BatchQueryResponse, error) {
	if p.isHA() && !p.hasLocalTSDB() {
		return p.forwardQueryBatch(ctx, req)
	}
	return p.runQueryBatch(ctx, req)
}

func (p *Plugin) hasLocalTSDB() bool {
	p.tsdbLock.RLock()
	defer p.tsdbLock.RUnlock()
	return p.db != nil
}

func (p *Plugin) runQueryBatch(ctx context.Context, req BatchQueryRequest) (BatchQueryResponse, error) {
	cfg, err := p.getConfiguration()
	if err != nil {
		return BatchQueryResponse{}, fmt.Errorf("could not read configuration: %w", err)
	}
	dataStart, err := p.DataStart()
	if err != nil {
		return BatchQueryResponse{}, fmt.Errorf("could not read tsdb start time: %w", err)
	}

	resp := BatchQueryResponse{
		Results:               make([]QueryResult, len(req.Queries)),
		DataStart:             dataStart,
		ScrapeIntervalSeconds: *cfg.ScrapeIntervalSeconds,
	}
	for i, expr := range req.Queries {
		resp.Results[i] = p.QueryRange(ctx, QueryRequest{
			Query: expr,
			Start: req.Start,
			End:   req.End,
			Step:  req.Step,
		})
	}

	return resp, nil
}
