package main

import (
	"bytes"
	"net/http"
	"path/filepath"
	"time"

	"github.com/gorilla/mux"

	root "github.com/mattermost/mattermost-plugin-metrics"

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
	appCfg := h.plugin.API.GetConfig()
	metricsFrom, ok := appCfg.PluginSettings.Plugins[root.Manifest.Id]["collect_metrics_from"]
	if !ok {
		// set it to the default value if the setting is not there
		metricsFrom = "yesterday"
	}
	var days int
	switch metricsFrom {
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
		err := h.plugin.fileBackend.RemoveDirectory(fp)
		if err != nil {
			h.plugin.API.LogError("Unable to remove temp directory for the dump", "error", err.Error())
		}
	}()

	b, err := h.plugin.fileBackend.ReadFile(fp)
	if err != nil {
		h.plugin.API.LogError("Failed to read dump file", "error", err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	web.WriteFileResponse(filepath.Base(fp), "application/zip", 0, max, *appCfg.ServiceSettings.WebserverMode, bytes.NewReader(b), true, w, r)
}
