package index

import (
	"bufio"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestTools(t *testing.T) {
	t.Parallel()
	want := []string{"start", "agentdex", "tk", "tkv", "kagi", "snag", "webctl", "hotseat", "p3bot"}
	if got := Names(); !slices.Equal(got, want) {
		t.Errorf("Names() = %v, want %v", got, want)
	}
	for _, tool := range Tools() {
		if !strings.HasPrefix(tool.Module, "github.com/p3bot/") {
			t.Errorf("%s: module %q is not under github.com/p3bot/", tool.Name, tool.Module)
		}
	}
	tk, _ := Lookup("tk")
	tkv, _ := Lookup("tkv")
	if tk.Module != "github.com/p3bot/tk" || tkv.Module != tk.Module {
		t.Errorf("tk %q and tkv %q must share github.com/p3bot/tk", tk.Module, tkv.Module)
	}
}

func TestToolsReturnsCopy(t *testing.T) {
	t.Parallel()
	Tools()[0].Name = "changed"
	if Names()[0] != "start" {
		t.Error("Tools() exposed the index to mutation")
	}
}

func TestLookup(t *testing.T) {
	t.Parallel()
	if got, ok := Lookup("p3bot"); !ok || got.Module != "github.com/p3bot/p3bot" {
		t.Errorf("Lookup(p3bot) = %+v, %v", got, ok)
	}
	for _, name := range []string{"", "library", "homebrew-tap", "TK"} {
		if got, ok := Lookup(name); ok {
			t.Errorf("Lookup(%q) = %+v, want not found", name, got)
		}
	}
}

// The AGENTS.md managed tool table must list the same binaries and modules,
// in the same order, so the two change in the same edit.
func TestAgentsTableMatchesIndex(t *testing.T) {
	t.Parallel()
	f, err := os.Open("../../AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	var rows []Tool
	inSection, inTable := false, false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "## "):
			inSection = line == "## Managed tools"
		case inSection && strings.HasPrefix(line, "|"):
			cells := strings.Split(strings.Trim(line, "|"), "|")
			for i := range cells {
				cells[i] = strings.Trim(strings.TrimSpace(cells[i]), "`")
			}
			if !inTable {
				if len(cells) != 2 || cells[0] != "Tool" || cells[1] != "Module" {
					t.Fatalf("managed tool table header = %v, want [Tool Module]", cells)
				}
				inTable = true
				continue
			}
			if strings.HasPrefix(cells[0], "---") {
				continue
			}
			rows = append(rows, Tool{Name: cells[0], Module: cells[1]})
		case inTable && line == "":
			inSection = false
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(rows, Tools()) {
		t.Errorf("AGENTS.md managed tools = %v\nindex                  = %v", rows, Tools())
	}
}
