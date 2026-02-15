//go:build linux

package core

import (
	linuxCollector "github.com/nicholasgasior/nvcheckup/internal/collector/linux"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

func collectPlatformSpecific(r *types.Report, cfg types.RunConfig) []types.CollectorError {
	linInfo, linErrs := linuxCollector.CollectLinuxInfo(cfg.Timeout, cfg.IncludeLogs)
	r.Linux = &linInfo
	return linErrs
}
