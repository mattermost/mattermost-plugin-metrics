package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/mattermost/mattermost/server/public/model"
	"github.com/mattermost/mattermost/server/public/plugin"
)

func (p *Plugin) GenerateSupportData(ctx *plugin.Context) ([]*model.FileData, error) {
	remoteStorageDir := filepath.Join(pluginDataDir, PluginName, tsdbDirName)
	dump, err := p.createDump(context.TODO(), ctx.RequestId, time.Now().Add(-1*time.Duration(*p.configuration.SupportPacketMetricsDays)*24*time.Hour), time.Now(), remoteStorageDir)
	if err != nil {
		return nil, fmt.Errorf("could not create dump: %w", err)
	}

	fr, err := p.fileBackend.Reader(dump)
	if err != nil {
		return nil, fmt.Errorf("could not read dump: %w", err)
	}
	defer fr.Close()

	b, err := io.ReadAll(fr)
	if err != nil {
		return nil, fmt.Errorf("could not read dump file into byte slice: %w", err)
	}

	return []*model.FileData{{
		Filename: filepath.Base(dump),
		Body:     b,
	},
	}, nil
}
