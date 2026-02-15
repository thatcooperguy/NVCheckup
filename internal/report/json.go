package report

import (
	"encoding/json"

	"github.com/nicholasgasior/nvcheckup/pkg/types"
)

// GenerateJSON produces a structured JSON report.
func GenerateJSON(report *types.Report) (string, error) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
