//go:build !windows && !linux

package remediate

import (
	"fmt"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// getAvailableActions returns nil on unsupported platforms. Every catalog
// entry (see catalog.go) targets Windows or Linux.
func getAvailableActions() []types.RemediationAction {
	return nil
}

// applyAction is a no-op on unsupported platforms. All action IDs are unknown.
func (e *Engine) applyAction(id string) (output string, undoInfo string, err error) {
	return "", "", fmt.Errorf("remediation actions are not supported on this platform (action: %q)", id)
}

// undoAction is a no-op on unsupported platforms. All action IDs are unknown.
func (e *Engine) undoAction(id string, undoInfo string) error {
	return fmt.Errorf("remediation undo is not supported on this platform (action: %q)", id)
}

// inspectAction is a no-op on unsupported platforms.
func (e *Engine) inspectAction(id string) (inspection, error) {
	return inspection{}, fmt.Errorf("remediation actions are not supported on this platform (action: %q)", id)
}
