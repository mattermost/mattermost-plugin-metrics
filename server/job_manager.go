package main

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

type JobManager struct {
	ticker    *time.Ticker
	closeChan chan bool
	wg        sync.WaitGroup
	plugin    *Plugin
}

// jobManager constantly polls the kvstore and starts the pending jobs
// if a job is in running state and not updated, manager will set its
// state to JobTimeout.
func (m *JobManager) run(ctx context.Context) {
	m.wg.Add(1)
	defer m.wg.Done()

	for {
		select {
		case <-m.ticker.C:
			jobs, err := m.plugin.GetAllJobs(context.TODO())
			if err != nil {
				m.plugin.API.LogError("manager could not retrieve dump jobs", "err", err)
			}

			unlock, err := m.plugin.lockJobKVMutex(ctx)
			if err != nil {
				m.plugin.API.LogError("manager could not acquire job lock", "err", err)
				continue
			}
			for _, job := range jobs {
				switch job.Status {
				case JobPending:
					if job.UpdateAt < time.Now().Add(-5*time.Minute).UnixMilli() {
						m.plugin.API.LogWarn("Setting obsolete job status to fail", "id", job.ID)
						job.Status = JobTimeout
						continue
					}
					m.plugin.API.LogInfo("job pending", "id", job.ID)
				case JobRunning:
					// check if it needs to be timed out
				default:
					continue
				}
			}
			b, err := json.Marshal(jobs)
			if err != nil {
				m.plugin.API.LogError("manager could not marshal jobs", "err", err)
				unlock()
				continue
			}
			appErr := m.plugin.API.KVSet(KVStoreJobKey, b)
			if appErr != nil {
				m.plugin.API.LogError("manager could not update job statuses", "err", appErr)
			}
			unlock()
		case <-m.closeChan:
			return
		}
	}
}

func (m *JobManager) stop() error {
	m.ticker.Stop()
	close(m.closeChan)

	m.wg.Wait()

	return nil
}
