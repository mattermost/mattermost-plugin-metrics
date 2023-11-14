package main

import (
	"testing"

	"github.com/mattermost/mattermost-plugin-metrics/server/mocks"
	"go.uber.org/mock/gomock"
)

func TestLog(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ml := mocks.NewMockLogger(ctrl)
	lg := &metricsLogger{api: ml}

	t.Run("should not panic on a single line", func(t *testing.T) {
		ml.EXPECT().LogInfo("test")
		lg.Log("test")
	})
	t.Run("generate info log", func(t *testing.T) {
		ml.EXPECT().LogInfo("a log")
		lg.Log("msg", "a log", "level", "info")
	})

	t.Run("generate error log", func(t *testing.T) {
		ml.EXPECT().LogError("a log")
		lg.Log("msg", "a log", "level", "error")
	})

	t.Run("shuffle order on error log", func(t *testing.T) {
		ml.EXPECT().LogError("a log")
		lg.Log("level", "error", "msg", "a log")
	})
}
