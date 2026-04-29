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
	p.tsdbLock.RLock()
	db := p.db
	engine := p.queryEngine
	p.tsdbLock.RUnlock()

	if engine == nil || db == nil {
		return QueryResult{Query: req.Query, Error: "query engine not available"}
	}

	start := time.Unix(req.Start, 0)
	end := time.Unix(req.End, 0)
	step := time.Duration(req.Step * float64(time.Second))
	if step < time.Second {
		step = time.Minute
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
