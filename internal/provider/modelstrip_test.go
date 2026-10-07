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
	if got := StripPathsIn(config, "a", "sol"); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := StripPathsIn(config, "b", "sol"); len(got) != 0 {
		t.Fatalf("other provider affected: %v", got)
	}
	if err := SetModelStrip("a/sol", []string{"thinking", "bad..path"}); err == nil {
		t.Fatal("invalid path accepted")
	}
	if got := StripPathsIn(settings.Load(), "a", "sol"); !slices.Equal(got, want) {
		t.Fatalf("failed save changed settings: %v", got)
	}
	config.RenamePerModel("a", "renamed")
	if got := StripPathsIn(config, "renamed", "sol"); !slices.Equal(got, want) {
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
	if got := StripPathsIn(settings.Load(), "a", "sol"); !slices.Equal(got, []string{"reasoning_effort"}) {
		t.Fatalf("reset lost inherited paths: %v", got)
	}
	if err := SetModelStrip("A/sol", []string{"thinking"}); err != nil {
		t.Fatal(err)
	}
	if err := Rename("a", "renamed"); err != nil {
		t.Fatal(err)
	}
	if got := StripPathsIn(settings.Load(), "renamed", "sol"); !slices.Equal(got, []string{"reasoning_effort", "thinking"}) {
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

func TestModelStripInheritance(t *testing.T) {
	prefsHome(t)
	global, own := []string{"reasoning"}, []string{"metadata"}
	no, yes := false, true
	if err := SetModelStrip("a/*", global); err != nil {
		t.Fatal(err)
	}
	if err := SetStripPreference("a/sol", &own, &no); err != nil {
		t.Fatal(err)
	}
	check := func(want []string, inherit bool) {
		t.Helper()
		s := settings.Load()
		if got := StripPathsIn(s, "a", "sol"); !slices.Equal(got, want) || StripInheritsIn(s, "a", "sol") != inherit {
			t.Fatalf("got %v, inheritance %v; want %v, %v", got, StripInheritsIn(s, "a", "sol"), want, inherit)
		}
	}
	check(own, false)
	if err := SetModelStrip("a/sol", nil); err != nil {
		t.Fatal(err)
	}
	check(nil, false)
	if err := SetModelPrefs("a", map[string]ModelPref{"sol": {Name: new(string)}}); err != nil {
		t.Fatal(err)
	}
	check(nil, false) // an older editor's unrelated save keeps the opt-out
	bad := []string{"bad..path"}
	if err := SetStripPreference("a/sol", &bad, &yes); err == nil {
		t.Fatal("invalid paths accepted")
	}
	check(nil, false)
	if err := SetStripPreference("a/*", nil, &no); err == nil {
		t.Fatal("provider inheritance accepted")
	}
	if err := SetStripPreference("a/sol", nil, &yes); err != nil {
		t.Fatal(err)
	}
	check(global, true)
	if err := SetStripPreference("a/sol", &own, &no); err != nil {
		t.Fatal(err)
	}
	copyID, err := AddCopy(Provider{ID: "copy", Name: "Copy", Chat: "http://127.0.0.1:1/v1", Models: []string{"sol"}}, "a")
	if err != nil {
		t.Fatal(err)
	}
	s := settings.Load()
	if StripInheritsIn(s, copyID, "sol") || !slices.Equal(StripPathsIn(s, copyID, "sol"), own) || !slices.Equal(s.ModelStrip[copyID+"/*"], global) {
		t.Fatal("copy lost strip preferences")
	}
	if err := Rename("a", "renamed"); err != nil {
		t.Fatal(err)
	}
	s = settings.Load()
	var carried settings.Settings
	settings.CarryPerModel(&carried, &s)
	if StripInheritsIn(carried, "renamed", "sol") || !slices.Equal(StripPathsIn(carried, "renamed", "sol"), own) {
		t.Fatal("rename/settings save lost opt-out")
	}
}

func TestModelStripBatchValidation(t *testing.T) {
	prefsHome(t)
	paths, bad := []string{"metadata"}, []string{"bad..path"}
	no := false
	err := SetModelPrefsWithStrip("a", &paths, map[string]ModelPref{"sol": {Strip: &bad, InheritStrip: &no}})
	if err == nil {
		t.Fatal("invalid batch accepted")
	}
	s := settings.Load()
	if len(s.ModelStrip) != 0 || len(s.ModelStripInherit) != 0 {
		t.Fatal("invalid batch partially saved")
	}
}

func TestModelStripUnsupportedBackend(t *testing.T) {
	paths, empty := []string{"metadata"}, []string{}
	no, yes := false, true
	p := Provider{ID: "builtin", Name: "Builtin", Account: &Account{Agent: "claude"}}
	if p.SupportsStrip() || CheckStripPrefs(p, &paths, nil) == nil {
		t.Fatal("built-in backend accepted strip paths")
	}
	if err := CheckStripPrefs(p, nil, map[string]ModelPref{"m": {InheritStrip: &no}}); err == nil {
		t.Fatal("unsupported backend accepted opt-out")
	}
	if err := CheckStripPrefs(p, &empty, map[string]ModelPref{"m": {Strip: &empty, InheritStrip: &yes}}); err != nil {
		t.Fatal("stale preferences must remain resettable:", err)
	}
	p.Account.Agent = "plugin"
	if !p.SupportsStrip() {
		t.Fatal("plugin treated as built-in")
	}
}
