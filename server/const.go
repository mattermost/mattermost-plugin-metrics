package main

import (
	"github.com/prometheus/prometheus/model/labels"
)

var (
	sampleMutator func(labels.Labels) labels.Labels //nolint:unused
)
