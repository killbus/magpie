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
