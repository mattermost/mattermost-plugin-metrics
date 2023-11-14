package main

import (
	"fmt"

	"golang.org/x/exp/slices"
)

type Logger interface {
	LogDebug(msg string, keyValuePairs ...any)
	LogInfo(msg string, keyValuePairs ...any)
	LogError(msg string, keyValuePairs ...any)
	LogWarn(msg string, keyValuePairs ...any)
}

// metricsLogger is an adapter from go-kit/logger to plugin.API logger.
// The only method "Log" calculates the log level and so on and then
// passes the values to API logger.
type metricsLogger struct {
	api Logger
}

func (m *metricsLogger) Log(items ...any) error {
	// these are to avoid any possible panic scenarios
	if len(items) == 0 {
		return nil
	} else if len(items) == 1 {
		m.api.LogInfo(fmt.Sprintf("%v", items[0]))
		return nil
	}

	// here we try to find the index of the original message
	// the logger interface being used for the prometheus is
	// slightly different. Fortunately the tsdb logger is not
	// verbose though. A log message from tsdb should consist of
	// a msg key value, and meta information such as a timestamp
	// and a log level is also expected.
	// an example of a log entry contents are shown below:
	// ts=2023-11-02T23:01:13.057Z
	// caller=compact.go:464
	// level=info
	// component=tsdb
	// msg="compact blocks"
	// count=3
	// mint=1698926412248
	// maxt=1698948000000
	// ulid=01HE949VD3YTRWF8GXT2TC97KJ
	// duration=318.459333ms
	var msgIndex int
	for i, item := range items {
		if s, ok := item.(string); ok && s == "msg" {
			msgIndex = i
			break
		}
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
