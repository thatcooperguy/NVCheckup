package remediate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/thatcooperguy/nvcheckup/pkg/types"
)

// knowledgeAction mirrors one entry of knowledge/remediations.json.
type knowledgeAction struct {
	ID          string          `json:"id"`
	Title       string          `json:"title"`
	Risk        types.RiskLevel `json:"risk"`
	Platform    string          `json:"platform"`
	Category    string          `json:"category"`
	Description string          `json:"description"`
	DryRunDesc  string          `json:"dry_run_desc"`
	UndoDesc    string          `json:"undo_desc"`
	NeedsReboot bool            `json:"needs_reboot"`
	NeedsAdmin  bool            `json:"needs_admin"`
	FindingIDs  []string        `json:"finding_ids"`
}

func loadKnowledgeActions(t *testing.T) []knowledgeAction {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "knowledge", "remediations.json"))
	if err != nil {
		t.Fatalf("read remediations.json: %v", err)
	}
	var doc struct {
		Actions []knowledgeAction `json:"actions"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse remediations.json: %v", err)
	}
	if len(doc.Actions) == 0 {
		t.Fatal("remediations.json lists no actions")
	}
	return doc.Actions
}

// TestCatalog_MatchesKnowledgeFile pins the Go catalog to the canonical text
// in knowledge/remediations.json, field by field and in the same order.
func TestCatalog_MatchesKnowledgeFile(t *testing.T) {
	want := loadKnowledgeActions(t)
	got := Catalog()
	if len(got) != len(want) {
		t.Fatalf("Catalog() has %d actions, remediations.json has %d", len(got), len(want))
	}
	for i, w := range want {
		g := got[i]
		if g.ID != w.ID {
			t.Errorf("action %d: catalog id %q, json id %q (order must match)", i, g.ID, w.ID)
			continue
		}
		check := func(field string, gotV, wantV interface{}) {
			if gotV != wantV {
				t.Errorf("%s.%s:\n  catalog: %v\n  json:    %v", w.ID, field, gotV, wantV)
			}
		}
		check("Title", g.Title, w.Title)
		check("Risk", g.Risk, w.Risk)
		check("Platform", g.Platform, w.Platform)
		check("Category", g.Category, w.Category)
		check("Description", g.Description, w.Description)
		check("DryRunDesc", g.DryRunDesc, w.DryRunDesc)
		check("UndoDesc", g.UndoDesc, w.UndoDesc)
		check("NeedsReboot", g.NeedsReboot, w.NeedsReboot)
		check("NeedsAdmin", g.NeedsAdmin, w.NeedsAdmin)
		if g.Platform != "windows" && g.Platform != "linux" && g.Platform != "all" {
			t.Errorf("%s: unexpected platform %q", g.ID, g.Platform)
		}
	}
}

func TestCatalog_ReturnsCopy(t *testing.T) {
	c := Catalog()
	c[0].Title = "mutated"
	if a, _ := ActionByID(c[0].ID); a.Title == "mutated" {
		t.Error("Catalog() must return a copy, not the backing slice")
	}
}

func TestActionByID(t *testing.T) {
	for _, want := range Catalog() {
		got, ok := ActionByID(want.ID)
		if !ok {
			t.Errorf("ActionByID(%q) not found", want.ID)
			continue
		}
		if got != want {
			t.Errorf("ActionByID(%q) = %+v, want %+v", want.ID, got, want)
		}
	}
	if _, ok := ActionByID("no-such-action"); ok {
		t.Error("ActionByID must report unknown ids")
	}
	if _, ok := ActionByID(""); ok {
		t.Error("ActionByID must reject the empty id")
	}
}

// TestGetAvailableActions_IsCatalogFilteredByPlatform makes sure the
// platform files serve catalog entries rather than their own definitions.
func TestGetAvailableActions_IsCatalogFilteredByPlatform(t *testing.T) {
	avail := getAvailableActions()
	seen := map[string]bool{}
	for _, a := range avail {
		def, ok := ActionByID(a.ID)
		if !ok {
			t.Errorf("available action %q is not in the catalog", a.ID)
			continue
		}
		if a != def {
			t.Errorf("available action %q differs from its catalog entry:\n  avail:   %+v\n  catalog: %+v", a.ID, a, def)
		}
		if a.Platform != runtime.GOOS && a.Platform != "all" {
			t.Errorf("available action %q targets %q, but this is %s", a.ID, a.Platform, runtime.GOOS)
		}
		seen[a.ID] = true
	}
	for _, c := range Catalog() {
		if (c.Platform == runtime.GOOS || c.Platform == "all") && !seen[c.ID] {
			t.Errorf("catalog action %q targets %s but getAvailableActions() omitted it", c.ID, runtime.GOOS)
		}
	}
	switch runtime.GOOS {
	case "windows", "linux":
		if len(avail) == 0 {
			t.Errorf("expected actions on %s", runtime.GOOS)
		}
	default:
		if len(avail) != 0 {
			t.Errorf("expected no actions on %s, got %d", runtime.GOOS, len(avail))
		}
	}
}

// TestCatalog_EveryActionHasUndoValidation: validateUndoInfo is the gate
// between the journal and privileged writes; a catalog action without a case
// there could never be undone.
func TestCatalog_EveryActionHasUndoValidation(t *testing.T) {
	for _, a := range Catalog() {
		err := validateUndoInfo(a.ID, "definitely-not-valid-undo-info\x00")
		if err == nil {
			t.Errorf("%s: validateUndoInfo accepted garbage", a.ID)
		} else if strings.Contains(err.Error(), "unknown remediation action") {
			t.Errorf("%s: validateUndoInfo does not know this catalog action", a.ID)
		}
	}
}
