package main

import (
	"encoding/json"
	"net/http"
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

	jobs := root.PathPrefix("/jobs").Subrouter()
	jobs.HandleFunc("", handler.getAllJobsHandler).Methods(http.MethodGet)
	jobs.HandleFunc("/create", handler.createJobHandler).Methods(http.MethodPost)
	jobs.HandleFunc("/delete/{id:[A-Za-z0-9]+}", handler.deleteJobHandler).Methods(http.MethodDelete)
	jobs.HandleFunc("/deleteAll", handler.deleteAllJobsHandler).Methods(http.MethodDelete)
	jobs.HandleFunc("/download/{id:[A-Za-z0-9]+}", handler.downloadJobHandler).Methods(http.MethodGet)

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

type JobCreateRequest struct {
	MinT int64 `json:"min_t"`
	MaxT int64 `json:"max_t"`
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

	err = json.NewEncoder(w).Encode(job)
	if err != nil {
		h.plugin.API.LogError("error while marshaling the job", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (h *handler) deleteJobHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if !model.IsValidId(id) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if err := h.plugin.DeleteJob(r.Context(), id); err != nil {
		h.plugin.API.LogError("error while job delete request", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (h *handler) deleteAllJobsHandler(w http.ResponseWriter, r *http.Request) {
	if err := h.plugin.DeleteAllJobs(r.Context()); err != nil {
		h.plugin.API.LogError("error while job delete all jobs request", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
}

func (h *handler) getAllJobsHandler(w http.ResponseWriter, r *http.Request) {
	jobs, err := h.plugin.GetAllJobs(r.Context())
	if err != nil {
		h.plugin.API.LogError("error while job list request", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	jobSlice := make([]*DumpJob, 0, len(jobs))
	for _, job := range jobs {
		jobSlice = append(jobSlice, job)
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

func (h *handler) downloadJobHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if !model.IsValidId(id) {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	jobs, err := h.plugin.GetAllJobs(r.Context())
	if err != nil {
		h.plugin.API.LogError("error while job list request", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	job, ok := jobs[id]
	if !ok {
		h.plugin.API.LogError("could not find job", "id", id)
		w.WriteHeader(http.StatusNotFound)
		return
	}

	fr, err := h.plugin.fileBackend.Reader(job.DumpLocation)
	if err != nil {
		h.plugin.API.LogError("error while acquiring the file reader", "err", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	defer fr.Close()

	appCfg := h.plugin.API.GetConfig()
	web.WriteFileResponse(filepath.Base(job.DumpLocation), "application/zip", 0, time.Now(), *appCfg.ServiceSettings.WebserverMode, fr, true, w, r)
}
