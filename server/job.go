package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	root "github.com/mattermost/mattermost-plugin-metrics"
	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/pluginapi/cluster"
)

const (
	JobPending = "pending"
	JobRunning = "running"
	JobError   = "failed"
	JobTimeout = "timeout"
	JobSucess  = "success"

	JobLockKey    = PluginName + "_job"
	KVStoreJobKey = "jobs"
)

type DumpJob struct {
	ID           string `json:"ID"`
	Status       string `json:"Status"`
	CreateAt     int64  `json:"CreateAt"`
	UpdateAt     int64  `jsob:"UpdateAt"`
	MinT         int64  `json:"MinT"`
	MaxT         int64  `json:"MaxT"`
	DumpLocation string `json:"DumpLocation"`
}

// here it is required to acquire an exclusive lock to avoid
// race in an HA environment if there are parallel job processing requests
func (p *Plugin) lockJobKVMutex(ctx context.Context) (func(), error) {
	lock, err := cluster.NewMutex(p.API, root.Manifest.Id)
	if err != nil {
		return nil, fmt.Errorf("could not acquire lock: %w", err)
	}

	if err = lock.LockWithContext(ctx); err != nil {
		return nil, fmt.Errorf("could not lock the lock: %w", err)
	}

	return lock.Unlock, nil
}

func (p *Plugin) CreateJob(ctx context.Context, min, max int64) (*DumpJob, error) {
	unlock, err := p.lockJobKVMutex(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()

	job := &DumpJob{
		ID:       model.NewId(),
		Status:   JobPending,
		CreateAt: time.Now().UnixMilli(),
		MinT:     min,
		MaxT:     max,
	}

	b, appErr := p.API.KVGet(KVStoreJobKey)
	if appErr != nil {
		return nil, fmt.Errorf("could not retreive jobs: %w", appErr)
	}

	// var jobsRaw map[string]json.RawMessage
	jobs := make(map[string]*DumpJob)

	if len(b) > 0 {
		err = json.Unmarshal(b, &jobs)
		if err != nil {
			return nil, fmt.Errorf("could not unmarshal jobs: %w", err)
		}
	}

	jobs[job.ID] = job

	b, err = json.Marshal(jobs)
	if err != nil {
		return nil, fmt.Errorf("could not marshal jobs: %w", err)
	}

	appErr = p.API.KVSet(KVStoreJobKey, b)
	if appErr != nil {
		return nil, fmt.Errorf("could not store jobs: %w", appErr)
	}

	return job, nil
}

func (p *Plugin) GetAllJobs(ctx context.Context) (map[string]*DumpJob, error) {
	unlock, err := p.lockJobKVMutex(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()

	b, appErr := p.API.KVGet(KVStoreJobKey)
	if appErr != nil {
		return nil, fmt.Errorf("could not retreive jobs: %w", appErr)
	}

	var jobs map[string]*DumpJob
	err = json.Unmarshal(b, &jobs)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal jobs: %w", err)
	}

	return jobs, nil
}

func (p *Plugin) DeleteJob(ctx context.Context, id string) error {
	unlock, err := p.lockJobKVMutex(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	b, appErr := p.API.KVGet(KVStoreJobKey)
	if appErr != nil {
		return fmt.Errorf("could not retreive jobs: %w", appErr)
	}

	var jobs map[string]*DumpJob
	err = json.Unmarshal(b, &jobs)
	if err != nil {
		return fmt.Errorf("could not unmarshal jobs: %w", err)
	}

	if _, ok := jobs[id]; !ok {
		return fmt.Errorf("job does not exist: %s", id)
	}

	delete(jobs, id)

	b, err = json.Marshal(jobs)
	if err != nil {
		return fmt.Errorf("could not marshal jobs: %w", err)
	}

	appErr = p.API.KVSet(KVStoreJobKey, b)
	if appErr != nil {
		return fmt.Errorf("could not store jobs: %w", appErr)
	}

	return nil
}
