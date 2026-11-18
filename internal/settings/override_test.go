package settings

import (
	"encoding/json"
	"testing"
)

// The new per-model override fields are the settings' only json.RawMessage
// maps: the param_override JSON a model is given, kept as raw JSON, and the
// explicit opt-outs from a provider-level override. They are exercised here
// on their own, beside the generic per-model tests that reach them by the
// name-and-type rule.

// A model's override and its inheritance marker save and reload, the JSON
// by its content: the settings file is written pretty-printed, so the
// override is re-indented on the way out and back and compared as JSON, not
// as the bytes it came in as.
func TestModelOverrideSavesAndReloads(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var s Settings
	s.ModelOverride = map[string]json.RawMessage{
		"relay/gpt-5": json.RawMessage(`{"temperature":0.2,"operations":[{"path":"t","mode":"set","value":9}]}`),
		"relay/*":     json.RawMessage(`{"max_completion_tokens":1200}`),
		"claude/opus": json.RawMessage(`{"operations":[{"path":"system","mode":"delete"}]}`),
	}
	s.ModelOverrideInherit = map[string]bool{
		"relay/sol": false,
	}
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if !perModelSame(got.ModelOverride, s.ModelOverride) {
		t.Fatalf("the override was not kept as it was: %v", got.ModelOverride)
	}
	if !perModelSame(got.ModelOverrideInherit, s.ModelOverrideInherit) {
		t.Fatalf("the inheritance marker was not kept: %v", got.ModelOverrideInherit)
	}
	// the nested object and the operations list survive whole, by content
	var one struct {
		Temperature  float64   `json:"temperature"`
		Operations   []any     `json:"operations"`
		ExtraNumbers []float64 `json:"extra"`
	}
	if err := json.Unmarshal(got.ModelOverride["relay/gpt-5"], &one); err != nil {
		t.Fatalf("the reloaded override does not parse: %v", err)
	}
	if one.Temperature != 0.2 || len(one.Operations) != 1 {
		t.Fatalf("the reloaded override lost content: %+v", one)
	}
}

// Saving the settings twice leaves the override as it was: a reload in
// between is not a change, so a page that saves its own choices does not
// rewrite the override into anything else.
func TestModelOverrideIdempotent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var s Settings
	s.ModelOverride = map[string]json.RawMessage{"relay/gpt-5": json.RawMessage(`{"a":{"b":[1,2,3]}}`)}
	if err := Save(s); err != nil {
		t.Fatal(err)
	}
	first := Load()
	if err := Save(first); err != nil {
		t.Fatal(err)
	}
	second := Load()
	if !perModelSame(second.ModelOverride, first.ModelOverride) {
		t.Fatalf("a second save changed the override: %v vs %v", first.ModelOverride, second.ModelOverride)
	}
}

// A page that sends only its own choices keeps the override, by the carry,
// as it does the names and levels: an unrelated save does not drop it.
func TestModelOverrideKeptByAnUnrelatedSave(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var want Settings
	want.ModelOverride = map[string]json.RawMessage{"relay/gpt-5": json.RawMessage(`{"temperature":0.2}`)}
	want.ModelOverrideInherit = map[string]bool{"relay/sol": false}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	// a theme-only change, as the Settings page sends it
	var in Settings
	in.Theme = "dark"
	cur := Load()
	in.AgentOrder, in.AgentsHidden, in.AgentsShown = cur.AgentOrder, cur.AgentsHidden, cur.AgentsShown
	in.Window = cur.Window
	in.Visible = cur.Visible
	CarryPerModel(&in, &cur)
	if err := Save(in); err != nil {
		t.Fatal(err)
	}
	got := Load()
	if got.Theme != "dark" {
		t.Fatalf("the page's own choice not saved: %+v", got)
	}
	if !perModelSame(got.ModelOverride, want.ModelOverride) {
		t.Fatalf("an unrelated save lost the override: %v", got.ModelOverride)
	}
	if !perModelSame(got.ModelOverrideInherit, want.ModelOverrideInherit) {
		t.Fatalf("an unrelated save lost the inheritance marker: %v", got.ModelOverrideInherit)
	}
}
