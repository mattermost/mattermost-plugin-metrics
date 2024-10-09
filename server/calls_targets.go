package main

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"time"

	promModel "github.com/prometheus/common/model"

	"github.com/mattermost/mattermost/server/public/model"
)

const (
	callsPluginID = "com.mattermost.calls"
)

func resolveURL(u string, timeout time.Duration) ([]net.IP, string, error) {
	parsed, err := url.Parse(u)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse url: %w", err)
	}

	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil {
		return nil, "", fmt.Errorf("failed to split host/port: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
	if err != nil {
		return nil, "", fmt.Errorf("failed to lookup ips: %w", err)
	}

	return ips, port, nil
}

func (p *Plugin) generateCallsTargets(appCfg *model.Config, host, port string, nodes []*model.ClusterDiscovery) ([]promModel.LabelSet, error) {
	// First, figure out if Calls is running. If so, add the plugin metrics endpoint to the targets.
	// Also, check if the external RTCD service is configured, in which case add its endpoints to targets.
	status, err := p.API.GetPluginStatus(callsPluginID)
	if err != nil {
		return nil, fmt.Errorf("generateCallsTargets: failed to get calls plugin status: %w", err)
	}

	if status.State != model.PluginStateRunning {
		p.API.LogDebug("generateCallsTargets: calls plugin is not running")
		return nil, nil
	}

	p.API.LogDebug("generateCallsTargets: calls plugin running, generating targets")

	var targets []promModel.LabelSet
	if len(nodes) < 2 {
		targets = []promModel.LabelSet{
			{
				promModel.AddressLabel:     promModel.LabelValue(net.JoinHostPort(host, port)),
				promModel.MetricsPathLabel: promModel.LabelValue(fmt.Sprintf("/plugins/%s/metrics", callsPluginID)),
				promModel.JobLabel:         "calls",
			},
		}
	} else {
		for i := range nodes {
			targets = append(targets, promModel.LabelSet{
				promModel.AddressLabel:     promModel.LabelValue(net.JoinHostPort(nodes[i].Hostname, port)),
				promModel.MetricsPathLabel: promModel.LabelValue(fmt.Sprintf("/plugins/%s/metrics", callsPluginID)),
				promModel.JobLabel:         "calls",
			})
		}
	}

	if appCfg.PluginSettings.Plugins[callsPluginID] != nil {
		rtcdURL, _ := appCfg.PluginSettings.Plugins[callsPluginID]["rtcdserviceurl"].(string)
		if rtcdURL != "" {
			// Since RTCD can be DNS load balanced, we need to resolve its hostname to figure out if there's more than a single node behind it.
			ips, port, err := resolveURL(rtcdURL, 5*time.Second)
			if err != nil {
				p.API.LogWarn("generateCallsTargets: failed to resolve rtcd URL", "err", err.Error())
			}

			for _, ip := range ips {
				targets = append(targets, promModel.LabelSet{
					promModel.AddressLabel: promModel.LabelValue(net.JoinHostPort(ip.String(), port)),
					promModel.JobLabel:     "calls",
				})
			}
		}
	}

	return targets, nil
}
