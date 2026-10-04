package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/settings"
)

func TestModelStripCmd(t *testing.T) {
	groupsHome(t)
	saidArgs(t, "strip", "a/*", "reasoning_effort")
	saidArgs(t, "strip", "a/m", "reasoning.effort,output_config.effort")
	output := saidArgs(t, "strip", "a/m")
	for _, part := range []string{"reasoning_effort", "reasoning.effort", "output_config.effort", "upstream default"} {
		if !strings.Contains(output, part) {
			t.Fatalf("missing %s in %s", part, output)
		}
	}
	if output := saidArgs(t, "strips"); !strings.Contains(output, "a/*:") || !strings.Contains(output, "a/m:") {
		t.Fatal(output)
	}
	if _, err := refusedArgs(t, "strip", "a/m", "bad..path"); err == nil {
		t.Fatal("invalid path accepted")
	}
	if got := settings.Load().ModelStrip["a/m"]; !slices.Equal(got, []string{"reasoning.effort", "output_config.effort"}) {
		t.Fatal(got)
	}
	if output := saidArgs(t, "strip", "a/m", "--reset"); !strings.Contains(output, "provider-wide paths still apply") || !strings.Contains(output, "reasoning_effort") {
		t.Fatal(output)
	}
	saidArgs(t, "strip", "a/*", "--reset")
	if len(settings.Load().ModelStrip) != 0 {
		t.Fatal("rules remain after reset")
	}
}

func TestModelStripInheritanceCmd(t *testing.T) {
	groupsHome(t)
	saidArgs(t, "strip", "a/*", "reasoning_effort")
	saidArgs(t, "strip", "a/m", "metadata", "--inherit=false")
	if out := saidArgs(t, "strip", "a/m"); strings.Contains(out, "reasoning_effort") || !strings.Contains(out, "metadata") || !strings.Contains(out, "inherit strip false") {
		t.Fatal(out)
	}
	saidArgs(t, "strip", "a/m", "--clear")
	if out := saidArgs(t, "strips"); !strings.Contains(out, "a/m:  (inherit strip false)") {
		t.Fatal(out)
	}
	if out := saidArgs(t, "strip", "a/m"); !strings.Contains(out, "no request fields stripped") {
		t.Fatal(out)
	}
	for _, args := range [][]string{{"strip", "a/*", "--inherit=false"}, {"strip", "a/m", "--inherit=no"}, {"strip", "a/m", "--reset", "--inherit=false"}} {
		if _, err := refusedArgs(t, args...); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	if out := saidArgs(t, "strip", "a/m", "--reset"); !strings.Contains(out, "reasoning_effort") {
		t.Fatal(out)
	}
	if len(settings.Load().ModelStripInherit) != 0 {
		t.Fatal("reset left inheritance override")
	}
	s := settings.Load()
	s.ModelStripInherit = map[string]bool{"gone/m": false}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	saidArgs(t, "strip", "gone/m", "--reset")
	if _, exists := settings.Load().ModelStripInherit["gone/m"]; exists {
		t.Fatal("could not reset removed provider's empty opt-out")
	}
}
