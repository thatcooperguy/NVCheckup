//go:build linux

package core

import (
	linuxCollector "github.com/nicholasgasior/nvcheckup/internal/collector/linux"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

func collectPlatformSpecific(r *types.Report, cfg types.RunConfig) []types.CollectorError {
	linInfo, linErrs := linuxCollector.CollectLinuxInfo(cfg.Timeout, cfg.IncludeLogs)
	r.Linux = &linInfo
	allErrs := linErrs

	// Collect display info
	if cfg.Mode == types.ModeGaming || cfg.Mode == types.ModeFull {
		displays, displayErrs := linuxCollector.CollectDisplayInfo(cfg.Timeout)
		r.Displays = displays
		allErrs = append(allErrs, displayErrs...)
	}

	// Collect Xid errors from kernel logs
	if cfg.Mode == types.ModeAI || cfg.Mode == types.ModeGaming || cfg.Mode == types.ModeFull {
		xidErrors, xidErrs := linuxCollector.CollectXidErrors(cfg.Timeout)
		if r.Linux != nil {
			r.Linux.XidErrors = xidErrors
		}
		allErrs = append(allErrs, xidErrs...)
	}

	// Detect llvmpipe software rendering fallback
	if cfg.Mode == types.ModeGaming || cfg.Mode == types.ModeAI || cfg.Mode == types.ModeFull {
		fallback, glRenderer, rendererErrs := linuxCollector.DetectLlvmpipe(cfg.Timeout)
		if r.Linux != nil {
			r.Linux.LlvmpipeFallback = fallback
			r.Linux.GLRenderer = glRenderer
		}
		allErrs = append(allErrs, rendererErrs...)
	}

	return allErrs
}
