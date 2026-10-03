package provider

import (
	"slices"
	"testing"

	"github.com/yetone/magpie/internal/settings"
)

func TestModelStripPreferences(t *testing.T) {
	prefsHome(t)
	if err := SetModelStrip("a/*", []string{"reasoning_effort"}); err != nil {
		t.Fatal(err)
	}
	if err := SetModelStrip("a/sol", []string{" reasoning.effort ", "reasoning_effort", "messages[].reasoning_content"}); err != nil {
		t.Fatal(err)
	}
	config := settings.Load()
	want := []string{"reasoning_effort", "reasoning.effort", "messages[].reasoning_content"}
	if got := StripPathsIn(config.ModelStrip, "a", "sol"); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := StripPathsIn(config.ModelStrip, "b", "sol"); len(got) != 0 {
		t.Fatalf("other provider affected: %v", got)
	}
	if err := SetModelStrip("a/sol", []string{"thinking", "bad..path"}); err == nil {
		t.Fatal("invalid path accepted")
	}
	if got := StripPathsIn(settings.Load().ModelStrip, "a", "sol"); !slices.Equal(got, want) {
		t.Fatalf("failed save changed settings: %v", got)
	}
	config.RenamePerModel("a", "renamed")
	if got := StripPathsIn(config.ModelStrip, "renamed", "sol"); !slices.Equal(got, want) {
		t.Fatalf("rename lost paths: %v", got)
	}
	var carried settings.Settings
	settings.CarryPerModel(&carried, &config)
	if !slices.Equal(carried.ModelStrip["renamed/sol"], config.ModelStrip["renamed/sol"]) {
		t.Fatal("settings save lost paths")
	}
	if err := SetModelStrip("a/sol", nil); err != nil {
		t.Fatal(err)
	}
	if got := StripPathsIn(settings.Load().ModelStrip, "a", "sol"); !slices.Equal(got, []string{"reasoning_effort"}) {
		t.Fatalf("reset lost inherited paths: %v", got)
	}
	if err := SetModelStrip("A/sol", []string{"thinking"}); err != nil {
		t.Fatal(err)
	}
	if err := Rename("a", "renamed"); err != nil {
		t.Fatal(err)
	}
	if got := StripPathsIn(settings.Load().ModelStrip, "renamed", "sol"); !slices.Equal(got, []string{"reasoning_effort", "thinking"}) {
		t.Fatalf("provider rename lost rules: %v", got)
	}
	config = settings.Load()
	config.ModelStrip["deleted/model"] = []string{"thinking"}
	if err := settings.Save(config); err != nil {
		t.Fatal(err)
	}
	if err := SetModelStrip("deleted/model", nil); err != nil {
		t.Fatal(err)
	}
	if _, found := settings.Load().ModelStrip["deleted/model"]; found {
		t.Fatal("deleted provider rule could not be reset")
	}
	if err := SetModelStrip("group/route", nil); err == nil {
		t.Fatal("group rules should be rejected")
	}
}

func TestCleanStripPaths(t *testing.T) {
	for _, path := range []string{"", ".field", "field.", "a..b", "items[]", "a[0].b", "a[*].b", "a[][].b", "*.b", "model", "model.field", "model[].field"} {
		if _, err := CleanStripPaths([]string{path}); err == nil {
			t.Errorf("accepted %q", path)
		}
	}
	paths := []string{"reasoning_effort", "reasoning.effort", "output_config.effort", "messages[].content[].text"}
	if clean, err := CleanStripPaths(paths); err != nil || !slices.Equal(clean, paths) {
		t.Fatalf("%v: %v", clean, err)
	}
}
