package main

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestResolveConflictingExposedLabels(t *testing.T) {
	lb := labels.NewBuilder(labels.FromMap(map[string]string{
		model.AddressLabel:        "test-address",
		model.ScrapeIntervalLabel: "1s",
		model.ScrapeTimeoutLabel:  "5s",
	}))

	t.Run("no conflicts", func(t *testing.T) {
		lbls := []labels.Label{}
		resolveConflictingExposedLabels(lb, lbls)

		require.Equal(t, 3, lb.Labels().Len()) // we defined 3 labels they are not conflicting
	})

	t.Run("conflicting on address label", func(t *testing.T) {
		lbls := []labels.Label{
			{Name: model.AddressLabel, Value: "new-address"},
		}
		resolveConflictingExposedLabels(lb, lbls)

		require.NotEmpty(t, lb.Get(model.AddressLabel))
		require.NotEmpty(t, lb.Get(model.ExportedLabelPrefix+model.AddressLabel))
		require.Equal(t, "new-address", lb.Get(model.ExportedLabelPrefix+model.AddressLabel))
	})

	t.Run("not conflicting, does not exist in the label set", func(t *testing.T) {
		lbls := []labels.Label{
			{Name: "new-key", Value: "new-address"},
		}
		resolveConflictingExposedLabels(lb, lbls)

		require.Empty(t, lb.Get("new-key"))
	})
}
