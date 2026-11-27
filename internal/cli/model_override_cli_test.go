package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

// The param override from the terminal: one JSON, saved, shown and taken
// away, checked by the engine's rules; there is no delete-only command.

func TestModelOverrideCmd(t *testing.T) {
	groupsHome(t)

	// saving one
	if err := modelCmd([]string{"override", "a/m", `{"temperature":0.2,"operations":[{"path":"max_tokens","mode":"set","value":8000}]}`}); err != nil {
		t.Fatal(err)
	}
	got := settings.Load().ModelOverride["a/m"]
	if len(got) == 0 || !strings.Contains(string(got), "temperature") {
		t.Fatalf("not saved: %s", got)
	}

	// showing it names it and prints the JSON
	out := saidArgs(t, "override", "a/m")
	if !strings.Contains(out, "a/m") || !strings.Contains(out, "temperature") {
		t.Fatalf("show: %q", out)
	}

	// the list shows it
	out = saidArgs(t, "overrides")
	if !strings.Contains(out, "a/m") {
		t.Fatalf("list: %q", out)
	}

	// an invalid JSON is refused
	if _, err := refusedArgs(t, "override", "a/m", `{"temperature":`); err == nil {
		t.Fatal("an invalid JSON was accepted")
	}
	// a root-model target is refused by the engine's rule
	if _, err := refusedArgs(t, "override", "a/m", `{"operations":[{"path":"model","mode":"set","value":"x"}]}`); err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("a root-model target: %v", err)
	}
	// an unknown mode is refused by name
	if _, err := refusedArgs(t, "override", "a/m", `{"operations":[{"path":"a","mode":"frobnicate"}]}`); err == nil || !strings.Contains(err.Error(), "frobnicate") {
		t.Fatalf("an unknown mode: %v", err)
	}

	// --reset takes it away
	if err := modelCmd([]string{"override", "a/m", "--reset"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := settings.Load().ModelOverride["a/m"]; ok {
		t.Fatal("still there after --reset")
	}

	// a provider-wide override, and a model shown to inherit it
	if err := modelCmd([]string{"override", "a/*", `{"temperature":0.1}`}); err != nil {
		t.Fatal(err)
	}
	out = saidArgs(t, "override", "a/only-a")
	if !strings.Contains(out, "a/*") {
		t.Fatalf("an inherited override not said as such: %q", out)
	}
	// the opt-out shows too
	if err := modelCmd([]string{"override", "a/only-a", `{"temperature":0.9}`}); err != nil {
		t.Fatal(err)
	}
	out = saidArgs(t, "override", "a/only-a")
	if !strings.Contains(out, "temperature") {
		t.Fatalf("the model's own override not shown: %q", out)
	}
	// a model the provider doesn't serve is refused
	if _, err := refusedArgs(t, "override", "a/nope", `{"temperature":0.2}`); err == nil {
		t.Fatal("an unknown model was accepted")
	}
}

// The inherit switch from the terminal: --no-inherit keeps a model out of its
// provider's override and --inherit puts it back; the opt-out is an entry of
// its own, so the model's own override is left as it is.
func TestModelOverrideCmdInherit(t *testing.T) {
	groupsHome(t)
	if err := modelCmd([]string{"override", "a/*", `{"temperature":0.1}`}); err != nil {
		t.Fatal(err)
	}
	if err := modelCmd([]string{"override", "a/m", `{"temperature":0.9}`}); err != nil {
		t.Fatal(err)
	}
	if err := modelCmd([]string{"override", "a/m", "--no-inherit"}); err != nil {
		t.Fatal(err)
	}
	if v, ok := settings.Load().ModelOverrideInherit["a/m"]; !ok || v {
		t.Fatalf("the opt-out is not stored: %v %v", v, ok)
	}
	// its own override is untouched by the opt-out
	if got := settings.Load().ModelOverride["a/m"]; !strings.Contains(string(got), "0.9") {
		t.Fatalf("the model's own override was lost: %s", got)
	}
	// the list names it apart, as a model with no override of its own to show
	out := saidArgs(t, "overrides")
	if !strings.Contains(out, "opted out") {
		t.Fatalf("the list doesn't say it opted out: %q", out)
	}
	// and the show agrees with what the engine applies: the model's own
	// override is in force, and the note says its provider's is not
	out = saidArgs(t, "override", "a/m")
	if !strings.Contains(out, "0.9") || !strings.Contains(out, "kept out of a/*") {
		t.Fatalf("show of an opted-out model with its own override: %q", out)
	}
	if got, ok := provider.ModelOverrideOf(settings.Load(), "a", "m"); !ok || !strings.Contains(string(got), "0.9") {
		t.Fatalf("the engine applies %s, want the model's own", got)
	}
	// a model kept out of its provider's override that has none of its own
	// says so, rather than showing the provider's
	if err := modelCmd([]string{"override", "a/m", "--reset"}); err != nil {
		t.Fatal(err)
	}
	out = saidArgs(t, "override", "a/m")
	if !strings.Contains(out, "opted out") || strings.Contains(out, "0.1") {
		t.Fatalf("show of an opted-out model with no own override: %q", out)
	}
	if _, ok := provider.ModelOverrideOf(settings.Load(), "a", "m"); ok {
		t.Fatal("the engine applies the provider's override to an opted-out model with none of its own")
	}
	// --inherit puts it back
	if err := modelCmd([]string{"override", "a/nope", "--no-inherit"}); err == nil {
		t.Fatal("a model the provider doesn't serve took an opt-out")
	}
	if err := modelCmd([]string{"override", "a/m", "--inherit"}); err != nil {
		t.Fatal(err)
	}
	if v, ok := settings.Load().ModelOverrideInherit["a/m"]; ok && !v {
		t.Fatal("still opted out after --inherit")
	}
}

// The stored override is the JSON given, by content: a save and reload keeps
// the operations and the nested values.
func TestModelOverrideCmdKeepsContent(t *testing.T) {
	groupsHome(t)
	given := `{"max_completion_tokens":1200,"operations":[{"path":"messages","mode":"append","value":{"role":"user","content":"x"}}]}`
	if err := modelCmd([]string{"override", "a/m", given}); err != nil {
		t.Fatal(err)
	}
	raw := settings.Load().ModelOverride["a/m"]
	var got, want any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(given), &want); err != nil {
		t.Fatal(err)
	}
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	if string(g) != string(w) {
		t.Fatalf("stored %s, want %s", g, w)
	}
}
