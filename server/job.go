package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	root "github.com/mattermost/mattermost-plugin-metrics"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/pluginapi/cluster"
)

const (
	JobScheduled = "scheduled"
	JobError     = "failed"
	JobSucess    = "success"

	JobLockKey    = PluginName + "_job_lock"
	KVStoreJobKey = PluginName + "_finished_jobs"
)

type DumpJob struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	CreateAt     int64  `json:"create_at"`
	MinT         int64  `json:"min_t"`
	MaxT         int64  `json:"max_t"`
	DumpLocation string `json:"dump_location"`
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

func (p *Plugin) JobCallback(_ string, job any) {
	dumpJob := job.(*DumpJob)
	defer p.FinishJob(context.TODO(), dumpJob)
	dumpJob.Status = JobError

	remoteStorageDir := filepath.Join(pluginDataDir, PluginName, tsdbDirName)
	dump, err := p.createDump(context.TODO(), dumpJob.ID, time.UnixMilli(dumpJob.MinT), time.UnixMilli(dumpJob.MaxT), remoteStorageDir)
	if err != nil {
		p.API.LogError("could not create dump", "err", err)
		return
	}

	dumpJob.DumpLocation = dump
	dumpJob.Status = JobSucess
}

func (p *Plugin) CreateJob(_ context.Context, min, max int64) (*DumpJob, error) {
	jobID := model.NewId()
	job := &DumpJob{
		ID:       jobID,
		Status:   JobScheduled,
		CreateAt: time.Now().UnixMilli(),
		MinT:     min,
		MaxT:     max,
	}

	_, err := p.scheduler.ScheduleOnce(job.ID, time.Now(), job)
	if err != nil {
		return nil, err
	}

	return job, nil
}

func (p *Plugin) GetAllJobs(ctx context.Context) (map[string]*DumpJob, error) {
	metas, err := p.scheduler.ListScheduledJobs()
	if err != nil {
		return nil, err
	}

	// get active jobs
	jobs := make(map[string]*DumpJob)
	for _, meta := range metas {
		prop, ok := meta.Props.(map[string]any)
		if !ok {
			p.API.LogError("could not cast props into map job", "props", prop)
			continue
		}

		b, err2 := json.Marshal(prop)
		if err2 != nil {
			p.API.LogError("could not marshal props", "id", meta.Key, "err", err2)
			continue
		}

		var j DumpJob
		if err2 := json.Unmarshal(b, &j); err2 != nil {
			p.API.LogError("could not unmarshal job", "id", meta.Key, "err", err2)
			continue
		}

		jobs[meta.Key] = &j
	}

	// we also get the finishedjobs
	unlock, err := p.lockJobKVMutex(ctx)
	if err != nil {
		return nil, err
	}
	defer unlock()

	b, appErr := p.API.KVGet(KVStoreJobKey)
	if appErr != nil {
		return nil, fmt.Errorf("could not retrieve jobs: %w", appErr)
	}

	if len(b) > 0 {
		var finishedJobs map[string]*DumpJob
		err = json.Unmarshal(b, &finishedJobs)
		if err != nil {
			return nil, fmt.Errorf("could not unmarshal jobs: %w", err)
		}

		for id, job := range finishedJobs {
			jobs[id] = job
		}
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
		return fmt.Errorf("could not retrieve jobs: %w", appErr)
	}

	var jobs map[string]*DumpJob
	err = json.Unmarshal(b, &jobs)
	if err != nil {
		return fmt.Errorf("could not unmarshal jobs: %w", err)
	}

	if _, ok := jobs[id]; !ok {
		// job doesn't exists in finished jobs
		// it's most likely a shceduledjob we hand it over to
		// scheduler to delete it
		p.API.LogDebug("canceling scheduled job", "id", id)
		p.scheduler.Cancel(id)
		return nil
	}
	err = p.fileBackend.RemoveDirectory(filepath.Dir(jobs[id].DumpLocation))
	if err != nil {
		return err
	}
	p.API.LogDebug("dump deleted", "id", jobs[id].ID, "file", jobs[id].DumpLocation)

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

func (p *Plugin) FinishJob(_ context.Context, job *DumpJob) error {
	b, appErr := p.API.KVGet(KVStoreJobKey)
	if appErr != nil {
		return fmt.Errorf("could not retrieve jobs: %w", appErr)
	}

	jobs := make(map[string]*DumpJob)

	if len(b) > 0 {
		err := json.Unmarshal(b, &jobs)
		if err != nil {
			return fmt.Errorf("could not unmarshal jobs: %w", err)
		}
	}

	jobs[job.ID] = job
	b, err := json.Marshal(jobs)
	if err != nil {
		return fmt.Errorf("could not marshal jobs: %w", err)
	}

	appErr = p.API.KVSet(KVStoreJobKey, b)
	if appErr != nil {
		return fmt.Errorf("could not store jobs: %w", appErr)
	}

	return nil
}
