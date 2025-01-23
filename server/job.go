// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	root "github.com/mattermost/mattermost-plugin-metrics"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/pluginapi/cluster"
)

const (
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
	lock, err := cluster.NewMutex(p.API, root.Manifest.Id+JobLockKey)
	if err != nil {
		return nil, fmt.Errorf("could not acquire lock: %w", err)
	}

	if err = lock.LockWithContext(ctx); err != nil {
		return nil, fmt.Errorf("could not lock the lock: %w", err)
	}

	return lock.Unlock, nil
}

func (p *Plugin) JobCallback(_ string, job any) {
	dumpJob, ok := job.(*DumpJob)
	if !ok {
		p.API.LogError("could not cast to DumpJob")
		return
	}

	dumpJob.Status = model.JobStatusInProgress
	err := p.UpdateJob(context.TODO(), dumpJob)
	if err != nil {
		p.API.LogError("could not update job status", "err", err)
		return
	}

	defer func() {
		err = p.UpdateJob(context.TODO(), dumpJob)
		if err != nil {
			p.API.LogError("could not update job status", "err", err)
			return
		}
	}()

	remoteStorageDir := filepath.Join(pluginDataDir, PluginName, tsdbDirName)
	dump, err := p.createDump(context.TODO(), dumpJob.ID, time.UnixMilli(dumpJob.MinT), time.UnixMilli(dumpJob.MaxT), remoteStorageDir)
	if err != nil {
		dumpJob.Status = model.JobStatusError
		p.API.LogError("could not create dump", "err", err)
		return
	}

	dumpJob.DumpLocation = dump.Path
	dumpJob.MinT = dump.MinT
	dumpJob.MaxT = dump.MaxT
	dumpJob.Status = model.JobStatusSuccess
}

func (p *Plugin) CreateJob(_ context.Context, min, max int64) (*DumpJob, error) {
	jobID := model.NewId()
	job := &DumpJob{
		ID:       jobID,
		Status:   model.JobStatusPending,
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
			p.API.LogWarn("could not cast props into string map", "props", prop)
			continue
		}

		b, err2 := json.Marshal(prop)
		if err2 != nil {
			p.API.LogWarn("could not marshal props", "id", meta.Key, "err", err2)
			continue
		}

		var j DumpJob
		if err2 := json.Unmarshal(b, &j); err2 != nil {
			// this is an expected case if the job props are not a DumpJob compatible type
			// we continue to parse other jobs to get actual dump jobs.
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

	ok, err := jobs[id].DeleteDump(p)
	if err != nil {
		p.API.LogError("dump could not be deleted", "id", jobs[id].ID, "err", err.Error())
	} else if ok {
		p.API.LogDebug("dump deleted", "id", jobs[id].ID, "file", jobs[id].DumpLocation)
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

func (p *Plugin) DeleteAllJobs(ctx context.Context) error {
	jobs, err := p.GetAllJobs(ctx)
	if err != nil {
		return err
	}

	for id := range jobs {
		err = p.DeleteJob(ctx, id)
		if err != nil {
			return err
		}
	}

	// this is danger zone but it is necessary to clear up any remaining files
	dumpDirectory := filepath.Join(pluginDataDir, PluginName, "dump")
	err = p.fileBackend.RemoveDirectory(dumpDirectory)
	if err != nil {
		return err
	}
	p.API.LogInfo("Dump directory removed from the file store.")

	return nil
}

func (p *Plugin) UpdateJob(ctx context.Context, job *DumpJob) error {
	unlock, err := p.lockJobKVMutex(ctx)
	if err != nil {
		return err
	}
	defer unlock()

	b, appErr := p.API.KVGet(KVStoreJobKey)
	if appErr != nil {
		return fmt.Errorf("could not retrieve jobs: %w", appErr)
	}

	jobs := make(map[string]*DumpJob)

	if len(b) > 0 {
		err = json.Unmarshal(b, &jobs)
		if err != nil {
			return fmt.Errorf("could not unmarshal jobs: %w", err)
		}
	}

	jobs[job.ID] = job
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

func (j *DumpJob) DeleteDump(p *Plugin) (bool, error) {
	// do not delete if the dump location is not under the plugin-data directory.
	//
	// there is a corner case that is; if the job receives a deletion request while not finished yet,
	// the dump location will be empty and therefore `filepath.Dir(jobs[id].DumpLocation)`
	// will return "." which would implicitly tell fileBackend to delete entire data directory.
	// to avoid this issue, we are checking the leading directory.
	if loc := j.DumpLocation; strings.HasPrefix(loc, filepath.Join(pluginDataDir, PluginName)) {
		err := p.fileBackend.RemoveDirectory(filepath.Dir(j.DumpLocation))
		if err != nil {
			return false, err
		}
		return true, nil
	}

	// there were no dump under the plugin-data/mattermost-plugin-metrics for this job
	// so it's been ignored.
	return false, nil
}
