// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/mattermost/mattermost-plugin-metrics/server/mocks"
)

func TestLog(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	ml := mocks.NewMockLogger(ctrl)
	lg := &metricsLogger{api: ml}

	t.Run("should not panic on a single line or empty line", func(t *testing.T) {
		ml.EXPECT().LogInfo("test")
		err := lg.Log("test")
		require.NoError(t, err)

		ml.EXPECT().LogInfo("")
		err = lg.Log("")
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

	t.Run("more fields with warning", func(t *testing.T) {
		ml.EXPECT().LogWarn("test", "mint", 1698926412248, "ulid", "01HE949VD3YTRWF8GXT2TC97KJ")
		err := lg.Log("mint", 1698926412248, "ulid", "01HE949VD3YTRWF8GXT2TC97KJ", "msg", "test", "level", "warn")
		require.NoError(t, err)
	})
}
