package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gorilla/mux"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/v8/platform/shared/web"
)

type handler struct {
	plugin *Plugin
	router *mux.Router
}

// newHandler constructs a new handler.
func newHandler(plugin *Plugin) *handler {
	handler := &handler{
		plugin: plugin,
	}

	root := mux.NewRouter()
	root.Use(handler.authorized)

	root.HandleFunc("/download", handler.downloadDumpHandler).Methods(http.MethodGet)

	jobs := root.PathPrefix("/jobs").Subrouter()
	jobs.HandleFunc("", handler.getAllJobsHandler).Methods(http.MethodGet)
	jobs.HandleFunc("/create", handler.createJobHandler).Methods(http.MethodPost)
	jobs.HandleFunc("/delete/{id:[A-Za-z0-9]+}", handler.deleteJobHandler).Methods(http.MethodDelete)

	handler.router = root

	return handler
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.router.ServeHTTP(w, r)
}

func (h *handler) authorized(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Header.Get("Mattermost-User-Id")
		if userID == "" {
			http.Error(w, "Not authorized", http.StatusUnauthorized)
			return
		}

		if !h.plugin.API.HasPermissionTo(userID, model.PermissionManageSystem) {
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (h *handler) downloadDumpHandler(w http.ResponseWriter, r *http.Request) {
	var days int
	switch *h.plugin.configuration.CollectMetricsFrom {
	case "yesterday":
		days = -1
	case "3_days":
		days = -3
	case "last_week":
		days = -7
	case "2_weeks":
		days = -14
	}

	min := time.Now().AddDate(0, 0, days)
	max := time.Now()

	remoteStorageDir := filepath.Join(pluginDataDir, PluginName, tsdbDirName)
	fp, err := h.plugin.createDump(r.Context(), min, max, remoteStorageDir)
	if err != nil {
		h.plugin.API.LogError("Failed to created dump", "error", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer func() {
		fErr := os.RemoveAll(filepath.Dir(fp))
		if fErr != nil {
			h.plugin.API.LogError("Unable to remove temp directory for the dump", "error", fErr.Error())
		}
	}()

	f, err := os.Open(fp)
	if err != nil {
		h.plugin.API.LogError("Failed to read dump file", "error", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer f.Close()

	appCfg := h.plugin.API.GetConfig()
	web.WriteFileResponse(filepath.Base(fp), "application/zip", 0, max, *appCfg.ServiceSettings.WebserverMode, f, true, w, r)
}

type JobCreateRequest struct {
	MinT int64
	MaxT int64
}

func (h *handler) createJobHandler(w http.ResponseWriter, r *http.Request) {
	var jcr JobCreateRequest
	err := json.NewDecoder(r.Body).Decode(&jcr)
	if err != nil {
		h.plugin.API.LogError("error while processing the request", "err", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	job, err := h.plugin.CreateJob(r.Context(), jcr.MinT, jcr.MaxT)
	if err != nil {
		h.plugin.API.LogError("error while job create request", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	b, err := json.Marshal(job)
	if err != nil {
		h.plugin.API.LogError("error while marshaling the job", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Write(b)
}

func (h *handler) deleteJobHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if !model.IsValidId(id) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if err := h.plugin.DeleteJob(r.Context(), id); err != nil {
		h.plugin.API.LogError("error while job create request", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (h *handler) getAllJobsHandler(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.plugin.GetAllJobs(r.Context())
	if err != nil {
		h.plugin.API.LogError("error while job create request", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	jobSlice := make([]*DumpJob, len(jobs))
	i := 0
	for id := range jobs {
		jobSlice[i] = jobs[id]
		i++
	}

	sort.Slice(jobSlice, func(i, j int) bool {
		return jobSlice[i].CreateAt > jobSlice[j].CreateAt
	})

	b, err := json.Marshal(jobSlice)
	if err != nil {
		h.plugin.API.LogError("error while marshaling the jobs", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Write(b)
}
