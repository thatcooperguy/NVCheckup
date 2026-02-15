//go:build linux

package linux

import (
	"os"
	"strings"

	"github.com/nicholasgasior/nvcheckup/internal/util"
	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// DetectLlvmpipe checks whether the system is using a software OpenGL
// renderer (llvmpipe or softpipe) instead of hardware-accelerated rendering.
// It returns whether a software fallback is active, the GL renderer string,
// and any collector errors encountered.
func DetectLlvmpipe(timeout int) (fallback bool, glRenderer string, errs []types.CollectorError) {
	// Step 1: Check glxinfo for the OpenGL renderer string
	if util.CommandExists("glxinfo") {
		r := util.RunCommand(timeout, "sh", "-c", `glxinfo 2>/dev/null | grep "OpenGL renderer"`)
		if r.Err != nil {
			errs = append(errs, types.CollectorError{
				Collector: "linux.renderer.glxinfo",
				Error:     "glxinfo failed: " + r.Err.Error(),
			})
		} else {
			output := strings.TrimSpace(r.Stdout)
			if output != "" {
				// Line format: "OpenGL renderer string: Mesa Intel(R) ..."
				// or: "OpenGL renderer string: llvmpipe (LLVM 15.0.7, 256 bits)"
				parts := strings.SplitN(output, ":", 2)
				if len(parts) == 2 {
					glRenderer = strings.TrimSpace(parts[1])
				} else {
					glRenderer = output
				}

				lower := strings.ToLower(glRenderer)
				if strings.Contains(lower, "llvmpipe") || strings.Contains(lower, "softpipe") {
					fallback = true
				}
			}
		}
	} else {
		errs = append(errs, types.CollectorError{
			Collector: "linux.renderer.glxinfo",
			Error:     "glxinfo not found; install mesa-utils to detect software rendering",
		})
	}

	// Step 2: Check LIBGL_ALWAYS_SOFTWARE environment variable
	if os.Getenv("LIBGL_ALWAYS_SOFTWARE") == "1" {
		fallback = true
		if glRenderer == "" {
			glRenderer = "unknown (LIBGL_ALWAYS_SOFTWARE=1)"
		}
	}

	return fallback, glRenderer, errs
}
