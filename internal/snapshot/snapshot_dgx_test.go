package snapshot

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thatcooperguy/nvcheckup/internal/collector/common"
	"github.com/thatcooperguy/nvcheckup/internal/redact"
	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// TestCollectDGXSpark_SimRoot drives the dgx-spark snapshot step against the
// committed GB10 fixture tree with an empty PATH (parsers only, no host
// tools), then applies the default redaction, so the DGX OS fields Diff
// compares are proven present and the serial number is proven redacted.
func TestCollectDGXSpark_SimRoot(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".github", "fieldtest", "simroot", "gb10"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "etc", "dgx-release")); err != nil {
		t.Skipf("GB10 simroot fixture not present: %v", err)
	}
	t.Setenv(common.SimRootEnv, root)
	t.Setenv("PATH", t.TempDir())

	snap := types.Snapshot{Platform: &types.PlatformInfo{Class: common.ClassDGXSpark}}
	collectDGXSpark(5, &snap, snap.Platform)
	if snap.DGXOS == nil {
		t.Fatal("DGXOS not collected")
	}
	if snap.DGXOS.OTAVersion != "7.5.0" || snap.DGXOS.SWBuildVersion != "7.2.3" || snap.DGXOS.FastOSVersion != "1.91.51" || snap.DGXOS.Name != "DGX Spark" {
		t.Errorf("release fields: %+v", snap.DGXOS)
	}
	if !snap.DGXOS.DashboardPortOpen || snap.DGXOS.UnitsQueried {
		t.Errorf("port/units: %+v", snap.DGXOS)
	}
	if snap.Platform.ACPIThermalMC["thermal_zone0"] != 45000 {
		t.Errorf("host state not collected: %+v", snap.Platform.ACPIThermalMC)
	}

	redact.ApplyToSnapshot(&snap, redact.NewWithIdentity(true, "alice", "spark-a1b2", "/home/alice"))
	if snap.DGXOS.SerialNumber != "<serial>" {
		t.Errorf("serial = %q", snap.DGXOS.SerialNumber)
	}
	if snap.DGXOS.OTAVersion != "7.5.0" {
		t.Errorf("version altered by redaction: %q", snap.DGXOS.OTAVersion)
	}

	// The two halves diff cleanly against themselves and against a changed OTA.
	other := snap
	changed := *snap.DGXOS
	changed.OTAVersion = "7.6.0"
	other.DGXOS = &changed
	found := false
	for _, d := range Diff(&snap, &other) {
		if d.Field == "dgx_os.ota_version" && d.ValueA == "7.5.0" && d.ValueB == "7.6.0" {
			found = true
		}
	}
	if !found {
		t.Errorf("Diff did not report dgx_os.ota_version: %+v", Diff(&snap, &other))
	}
}
