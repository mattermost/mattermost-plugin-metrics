package main

import (
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/prometheus/prometheus/scrape"
	"golang.org/x/exp/slices"
)

func mutateSampleLabels(lset labels.Labels, target *scrape.Target, honor bool, rc []*relabel.Config) labels.Labels {
	lb := labels.NewBuilder(lset)

	if honor {
		target.LabelsRange(func(l labels.Label) {
			if !lset.Has(l.Name) {
				lb.Set(l.Name, l.Value)
			}
		})
	} else {
		var conflictingExposedLabels []labels.Label
		target.LabelsRange(func(l labels.Label) {
			existingValue := lset.Get(l.Name)
			if existingValue != "" {
				conflictingExposedLabels = append(conflictingExposedLabels, labels.Label{Name: l.Name, Value: existingValue})
			}
			// It is now safe to set the target label.
			lb.Set(l.Name, l.Value)
		})

		if len(conflictingExposedLabels) > 0 {
			resolveConflictingExposedLabels(lb, conflictingExposedLabels)
		}
	}

	res := lb.Labels()

	if len(rc) > 0 {
		res, _ = relabel.Process(res, rc...)
	}

	return res
}

func resolveConflictingExposedLabels(lb *labels.Builder, conflictingExposedLabels []labels.Label) {
	slices.SortStableFunc(conflictingExposedLabels, func(a, b labels.Label) bool {
		return len(a.Name) < len(b.Name)
	})

	for _, l := range conflictingExposedLabels {
		newName := l.Name
		for {
			newName = model.ExportedLabelPrefix + newName
			if lb.Get(newName) == "" {
				lb.Set(newName, l.Value)
				break
			}
		}
	}
}
