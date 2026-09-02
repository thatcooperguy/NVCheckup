package report

import (
	"encoding/json"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// GenerateJSON produces a structured JSON report. The layout is described by
// metadata.schema_version; if the caller did not set it, the current schema
// version is filled in so consumers can always rely on the field.
func GenerateJSON(report *types.Report) (string, error) {
	if report.Metadata.SchemaVersion == "" {
		report.Metadata.SchemaVersion = types.SchemaVersion
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
