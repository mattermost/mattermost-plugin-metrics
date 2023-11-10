package main

import (
	"fmt"

	"github.com/mattermost/mattermost/server/public/plugin"
	"golang.org/x/exp/slices"
)

// metricsLogger is an adapter from go-kit/logger to plugin.API logger.
// The only method "Log" calculates the log level and so on and then
// passes the values to API logger.
type metricsLogger struct {
	api plugin.API
}

func (m *metricsLogger) Log(items ...any) error {
	// here we try to find the index of the original message
	// the logger interface being used for the prometheus is
	// slightly different. Fortunately the tsdb logger is not
	// verbose though.
	var msgIndex int
	for i, item := range items {
		if s, ok := item.(string); ok && s == "msg" {
			msgIndex = i
			break
		}
	}

	// these are to avoid any possible panic scenarios
	if len(items) == 0 {
		return nil
	} else if len(items) == 1 {
		m.api.LogInfo(fmt.Sprintf("%v", items[0]))
		return nil
	}

	// we get the message and then remove the key value pair from the items slice
	// then the removedd items will still be added to the list
	msg := items[msgIndex+1]
	kvps := slices.Delete[[]any](items, msgIndex, msgIndex+2)
	logFn := m.api.LogInfo

	// once we remove msg kv, indexes are spoiled, need to check level index again
	for i, item := range kvps {
		if s, ok := item.(string); ok && s == "level" {
			switch kvps[i+1] {
			case "debug":
				logFn = m.api.LogDebug
			case "warn":
				logFn = m.api.LogWarn
			case "error":
				logFn = m.api.LogError
			}
			// finally remove the "level" kv from the slice
			kvps = slices.Delete[[]any](kvps, i, i+2)
			break
		}
	}
	logFn(fmt.Sprintf("%s", msg), kvps...)

	return nil
}
