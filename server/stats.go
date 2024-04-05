package main

import (
	"path/filepath"
)

// TSDBStats
type TSDBStats struct {
	MinT       int64  `json:"min_t"`
	MaxT       int64  `json:"max_t"`
	NumSamples uint64 `json:"num_samples"`
	NumSeries  uint64 `json:"num_series"`
}

func (p *Plugin) GetTSDBStats() (TSDBStats, error) {
	stats := TSDBStats{}
	remoteStorageDir := filepath.Join(pluginDataDir, PluginName, tsdbDirName)
	blocks, err := p.fileBackend.ListDirectory(remoteStorageDir)
	if err != nil {
		return stats, err
	} else if len(blocks) == 0 {
		return stats, nil
	}

	for _, b := range blocks {
		meta, rErr := readBlockMeta(filepath.Join(b, metaFileName), p.fileBackend.ReadFile)
		if rErr != nil {
			p.API.LogDebug("unable to read meta file", "err", rErr)
			continue
		}

		if meta.MinTime < stats.MinT || stats.MinT == 0 {
			stats.MinT = meta.MinTime
		}

		if meta.MaxTime > stats.MaxT {
			stats.MaxT = meta.MaxTime
		}

		if meta.Stats.NumSeries > stats.NumSeries {
			stats.NumSeries = meta.Stats.NumSeries
		}

		stats.NumSamples += meta.Stats.NumSamples
	}

	return stats, nil
}
