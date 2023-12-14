package main

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"
)

const (
	scrapeAcceptHeader = `application/openmetrics-text;version=1.0.0,application/openmetrics-text;version=0.0.1;q=0.75,text/plain;version=0.0.4;q=0.5,*/*;q=0.1`
)

var (
	sampleMutator    func(labels.Labels) labels.Labels //nolint:unused
	errBodySizeLimit = errors.New("body size limit exceeded")
	UserAgent        = fmt.Sprintf("%s/%s", PluginName, ScraperVersion)
)
