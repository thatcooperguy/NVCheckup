//go:build windows

package core

import (
	winCollector "github.com/nicholasgasior/nvcheckup/internal/collector/windows"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

func collectPlatformSpecific(r *types.Report, cfg types.RunConfig) []types.CollectorError {
	if cfg.Mode == types.ModeGaming || cfg.Mode == types.ModeStreaming ||
		cfg.Mode == types.ModeCreator || cfg.Mode == types.ModeFull {
		winInfo, winErrs := winCollector.CollectWindowsInfo(cfg.Timeout, cfg.IncludeLogs)
		r.Windows = &winInfo
		return winErrs
	}
	return nil
}
