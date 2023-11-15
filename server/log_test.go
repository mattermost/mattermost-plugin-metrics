package main

import (
	"testing"

	"github.com/mattermost/mattermost-plugin-metrics/server/mocks"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestLog(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ml := mocks.NewMockLogger(ctrl)
	lg := &metricsLogger{api: ml}

	t.Run("should not panic on a single line", func(t *testing.T) {
		ml.EXPECT().LogInfo("test")
		err := lg.Log("test")
		require.NoError(t, err)
	})
	t.Run("generate info log", func(t *testing.T) {
		ml.EXPECT().LogInfo("a log")
		err := lg.Log("msg", "a log", "level", "info")
		require.NoError(t, err)
	})

	t.Run("generate error log", func(t *testing.T) {
		ml.EXPECT().LogError("a log")
		err := lg.Log("msg", "a log", "level", "error")
		require.NoError(t, err)
	})

	t.Run("shuffle order on error log", func(t *testing.T) {
		ml.EXPECT().LogError("a log")
		err := lg.Log("level", "error", "msg", "a log")
		require.NoError(t, err)
	})
}
