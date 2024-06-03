package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

func (p *Plugin) GenerateSupportData(_ *plugin.Context) ([]*model.FileData, error) {
	jobs, err := p.GetAllJobs(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("could not retrieve jobs")
	}

	jobSlice := make([]*DumpJob, 0, len(jobs))
	for _, job := range jobs {
		jobSlice = append(jobSlice, job)
	}

	sort.Slice(jobSlice, func(i, j int) bool {
		return jobSlice[i].CreateAt > jobSlice[j].CreateAt
	})

	var recentJob *DumpJob
	for _, j := range jobSlice {
		if j.Status == model.JobStatusSuccess {
			recentJob = j
			break
		}
	}

	if recentJob == nil {
		return nil, errors.New("there is no dumps in the filestore, please create a tsdb dump first")
	}
	dumpLocation := recentJob.DumpLocation

	fr, err := p.fileBackend.Reader(dumpLocation)
	if err != nil {
		return nil, fmt.Errorf("could not read dump: %w", err)
	}
	defer fr.Close()

	b, err := io.ReadAll(fr)
	if err != nil {
		return nil, fmt.Errorf("could not read dump file into byte slice: %w", err)
	}

	return []*model.FileData{{
		Filename: filepath.Base(dumpLocation),
		Body:     b,
	},
	}, nil
}
