// Copyright (c) 2023-present Mattermost, Inc. All Rights Reserved.
// See LICENSE.txt for license information.

package main

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"

	promModel "github.com/prometheus/common/model"

	pluginMocks "github.com/mattermost/mattermost-plugin-metrics/server/mocks/github.com/mattermost/mattermost/server/public/plugin"
)

func TestResolveURL(t *testing.T) {
	ips, port, err := resolveURL("https://localhost:8045", time.Second)
	require.NoError(t, err)
	require.NotEmpty(t, ips)
	require.Equal(t, "127.0.0.1", ips[0].String())
	require.Equal(t, "8045", port)

	ips, port, err = resolveURL("http://127.0.0.1:8055", time.Second)
	require.NoError(t, err)
	require.NotEmpty(t, ips)
	require.Equal(t, "127.0.0.1", ips[0].String())
	require.Equal(t, "8055", port)
}

func TestGenerateCallsTargets(t *testing.T) {
	mockAPI := &pluginMocks.MockAPI{}
	defer mockAPI.AssertExpectations(t)

	p := Plugin{
		MattermostPlugin: plugin.MattermostPlugin{
			API: mockAPI,
		},
	}

	cfg := &model.Config{}
	cfg.SetDefaults()

	t.Run("plugin not installed", func(t *testing.T) {
		mockAPI.On("GetPluginStatus", callsPluginID).Return(&model.PluginStatus{}, model.NewAppError("GetPluginStatus", "Plugin is not installed.", nil, "", http.StatusNotFound)).Once()

		targets, err := p.generateCallsTargets(cfg, "localhost", "8067", nil)
		require.EqualError(t, err, "generateCallsTargets: failed to get calls plugin status: GetPluginStatus: Plugin is not installed.")
		require.Empty(t, targets)
	})

	t.Run("plugin not running", func(t *testing.T) {
		mockAPI.On("GetPluginStatus", callsPluginID).Return(&model.PluginStatus{
			State: model.PluginStateStarting,
		}, nil).Once()
		mockAPI.On("LogDebug", "generateCallsTargets: calls plugin is not running").Return().Once()

		targets, err := p.generateCallsTargets(cfg, "localhost", "8067", nil)
		require.NoError(t, err)
		require.Empty(t, targets)
	})

	t.Run("single node", func(t *testing.T) {
		mockAPI.On("GetPluginStatus", callsPluginID).Return(&model.PluginStatus{
			State: model.PluginStateRunning,
		}, nil).Once()
		mockAPI.On("LogDebug", "generateCallsTargets: calls plugin running, generating targets").Return().Once()

		targets, err := p.generateCallsTargets(cfg, "localhost", "8067", nil)
		require.NoError(t, err)
		require.Equal(t, []promModel.LabelSet{
			{
				promModel.AddressLabel:     "localhost:8067",
				promModel.MetricsPathLabel: "/plugins/com.mattermost.calls/metrics",
				promModel.JobLabel:         "calls",
			},
		}, targets)
	})

	t.Run("multi node", func(t *testing.T) {
		mockAPI.On("GetPluginStatus", callsPluginID).Return(&model.PluginStatus{
			State: model.PluginStateRunning,
		}, nil).Once()
		mockAPI.On("LogDebug", "generateCallsTargets: calls plugin running, generating targets").Return().Once()

		targets, err := p.generateCallsTargets(cfg, "localhost", "8067", []*model.ClusterDiscovery{
			{
				Hostname: "192.168.1.1",
			},
			{
				Hostname: "192.168.1.2",
			},
		})
		require.NoError(t, err)
		require.Equal(t, []promModel.LabelSet{
			{
				promModel.AddressLabel:     "192.168.1.1:8067",
				promModel.MetricsPathLabel: "/plugins/com.mattermost.calls/metrics",
				promModel.JobLabel:         "calls",
			},
			{
				promModel.AddressLabel:     "192.168.1.2:8067",
				promModel.MetricsPathLabel: "/plugins/com.mattermost.calls/metrics",
				promModel.JobLabel:         "calls",
			},
		}, targets)
	})

	t.Run("rtcd", func(t *testing.T) {
		mockAPI.On("GetPluginStatus", callsPluginID).Return(&model.PluginStatus{
			State: model.PluginStateRunning,
		}, nil).Once()
		mockAPI.On("LogDebug", "generateCallsTargets: calls plugin running, generating targets").Return().Once()

		cfg.PluginSettings.Plugins[callsPluginID] = map[string]any{
			"rtcdserviceurl": "http://localhost:8045",
		}

		targets, err := p.generateCallsTargets(cfg, "localhost", "8067", nil)
		require.NoError(t, err)
		require.Equal(t, []promModel.LabelSet{
			{
				promModel.AddressLabel:     "localhost:8067",
				promModel.MetricsPathLabel: "/plugins/com.mattermost.calls/metrics",
				promModel.JobLabel:         "calls",
			},
			{
				promModel.AddressLabel: "127.0.0.1:8045",
				promModel.JobLabel:     "calls",
			},
		}, targets)
	})
}
