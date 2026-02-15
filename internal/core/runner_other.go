//go:build !windows && !linux

package core

import (
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

func collectPlatformSpecific(r *types.Report, cfg types.RunConfig) []types.CollectorError {
	return []types.CollectorError{{
		Collector: "platform",
		Error:     "unsupported platform: platform-specific collectors not available",
	}}
}
