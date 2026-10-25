package provider

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/settings"
)

// sameJSON is whether two override blobs are the same JSON, whatever the
// whitespace: the settings file is written pretty-printed, so a value read
// back is re-indented rather than the bytes it went in as.
func sameJSON(a, b json.RawMessage) bool {
	var x, y any
	if json.Unmarshal(a, &x) != nil || json.Unmarshal(b, &y) != nil {
		return false
	}
	return reflect.DeepEqual(x, y)
}

// setupOverride saves one provider that serves sol and luna, for the
// override lifecycle tests.
func setupOverride(t *testing.T) {
	t.Helper()
	limitsHome(t)
	if err := Save(Provider{ID: "relay", Name: "relay", Chat: "https://relay.example/v1", Key: "k", Models: []string{"sol", "luna"}}); err != nil {
		t.Fatal(err)
	}
	if err := Save(Provider{ID: "other", Name: "other", Chat: "https://other.example/v1", Key: "k", Models: []string{"sol"}}); err != nil {
		t.Fatal(err)
	}
}

// The lookup: a model's own override wins over its provider's, a provider's
// "*" covers every model of it, a missing one is nothing, and an explicit
// opt-out keeps the body as it is even where the provider has one.
func TestModelOverrideOf(t *testing.T) {
	setupOverride(t)
	mustSet := func(ref, ov string) {
		t.Helper()
		if err := SetModelOverride(ref, json.RawMessage(ov)); err != nil {
			t.Fatalf("set %s: %v", ref, err)
		}
	}
	mustSet("relay/*", `{"temperature":0.1}`)
	if raw, ok := ModelOverride("relay", "sol"); !ok || !sameJSON(raw, json.RawMessage(`{"temperature":0.1}`)) {
		t.Fatalf("provider wildcard: %q %v", raw, ok)
	}
	mustSet("relay/sol", `{"temperature":0.9}`)
	if raw, _ := ModelOverride("relay", "sol"); !sameJSON(raw, json.RawMessage(`{"temperature":0.9}`)) {
		t.Fatalf("model's own should win: %q", raw)
	}
	if raw, _ := ModelOverride("relay", "luna"); !sameJSON(raw, json.RawMessage(`{"temperature":0.1}`)) {
		t.Fatalf("the other model keeps the wildcard: %q", raw)
	}
	if _, ok := ModelOverride("other", "sol"); ok {
		t.Fatal("another provider's override leaked")
	}
	// the opt-out: sol keeps the body as it is, the wildcard notwithstanding
	if err := SetModelOverrideInherit("relay/sol", true); err != nil {
		t.Fatal(err)
	}
	// the model's own still wins over the opt-out, which is about the
	// provider's override, not the model's own
	if raw, ok := ModelOverride("relay", "sol"); !ok || !sameJSON(raw, json.RawMessage(`{"temperature":0.9}`)) {
		t.Fatalf("the opt-out took the model's own override too: %q %v", raw, ok)
	}
	if raw, ok := ModelOverride("relay", "luna"); !ok || !sameJSON(raw, json.RawMessage(`{"temperature":0.1}`)) {
		t.Fatalf("the opt-out reached another model: %q %v", raw, ok)
	}
	if opted, explicit := ModelOverrideInherit("relay", "sol"); !opted || !explicit {
		t.Fatalf("the opt-out reads %v %v", opted, explicit)
	}
	if opted, explicit := ModelOverrideInherit("relay", "luna"); opted || explicit {
		t.Fatalf("a model with no opt-out reads %v %v", opted, explicit)
	}
	// the opt-out keeps a model with no override of its own out of the
	// provider's
	if err := SetModelOverrideInherit("relay/luna", true); err != nil {
		t.Fatal(err)
	}
	if raw, ok := ModelOverride("relay", "luna"); ok {
		t.Fatalf("an opted-out model with no own override still had one: %q", raw)
	}
	if err := SetModelOverrideInherit("relay/luna", false); err != nil {
		t.Fatal(err)
	}
	if raw, ok := ModelOverride("relay", "luna"); !ok || !sameJSON(raw, json.RawMessage(`{"temperature":0.1}`)) {
		t.Fatalf("after dropping luna's opt-out: %q %v", raw, ok)
	}
	// giving the opt-out back gives the override back
	if err := SetModelOverrideInherit("relay/sol", false); err != nil {
		t.Fatal(err)
	}
	if raw, ok := ModelOverride("relay", "sol"); !ok || !sameJSON(raw, json.RawMessage(`{"temperature":0.9}`)) {
		t.Fatalf("after dropping the opt-out: %q %v", raw, ok)
	}
	if _, ok := settings.Load().ModelOverrideInherit["relay/sol"]; ok {
		t.Fatal("dropping the opt-out left a marker")
	}
}

// A save is checked by the engine's own rules first: a root-model target and
// an unknown mode are refused, and nothing is kept.
func TestSetModelOverrideValidates(t *testing.T) {
	setupOverride(t)
	if err := SetModelOverride("relay/sol", json.RawMessage(`{"operations":[{"path":"model","mode":"set","value":"x"}]}`)); err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("a root-model target: %v", err)
	}
	if err := SetModelOverride("relay/sol", json.RawMessage(`{"operations":[{"path":"a","mode":"frobnicate"}]}`)); err == nil || !strings.Contains(err.Error(), "frobnicate") {
		t.Fatalf("an unknown mode: %v", err)
	}
	if got := settings.Load().ModelOverride; len(got) != 0 {
		t.Fatalf("a refused save kept something: %v", got)
	}
	if err := SetModelOverride("relay/sol", json.RawMessage(`{"temperature":0.2}`)); err != nil {
		t.Fatalf("a valid one: %v", err)
	}
	if got := settings.Load().ModelOverride["relay/sol"]; len(got) == 0 {
		t.Fatalf("a valid save kept nothing")
	}
}

// A model the provider does not serve is refused, as a name or a price is;
// "*" is always allowed.
func TestSetModelOverrideRefusesUnknownModel(t *testing.T) {
	setupOverride(t)
	if err := SetModelOverride("relay/nope", json.RawMessage(`{"temperature":0.2}`)); err == nil {
		t.Fatal("an unknown model was accepted")
	}
	if err := SetModelOverride("relay/*", json.RawMessage(`{"temperature":0.2}`)); err != nil {
		t.Fatalf("the provider wildcard was refused: %v", err)
	}
	if err := SetModelOverride("nosuch/sol", json.RawMessage(`{"temperature":0.2}`)); err == nil {
		t.Fatal("an unknown provider was accepted")
	}
}

// An empty value and DropModelOverride both take it away, and each says
// whether there was one.
func TestDropModelOverride(t *testing.T) {
	setupOverride(t)
	if ok, err := DropModelOverride("relay/sol"); err != nil || ok {
		t.Fatalf("dropping one that was never there: %v %v", ok, err)
	}
	if err := SetModelOverride("relay/sol", json.RawMessage(`{"temperature":0.2}`)); err != nil {
		t.Fatal(err)
	}
	if ok, err := DropModelOverride("relay/sol"); err != nil || !ok {
		t.Fatalf("dropping one: %v %v", ok, err)
	}
	if _, ok := ModelOverride("relay", "sol"); ok {
		t.Fatal("it is still there")
	}
	// an empty value is the same as a drop
	if err := SetModelOverride("relay/sol", json.RawMessage(`{"temperature":0.2}`)); err != nil {
		t.Fatal(err)
	}
	if err := SetModelOverride("relay/sol", nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := settings.Load().ModelOverride["relay/sol"]; ok {
		t.Fatal("an empty value left one behind")
	}
	// an opt-out is a choice of its own: taking the model's own override
	// away leaves it standing, so its provider's does not come back in force
	if err := SetModelOverrideInherit("relay/sol", true); err != nil {
		t.Fatal(err)
	}
	if ok, _ := DropModelOverride("relay/sol"); ok {
		t.Fatal("a drop said it took an opt-out there was none of")
	}
	if v, ok := settings.Load().ModelOverrideInherit["relay/sol"]; !ok || v {
		t.Fatal("the opt-out was taken with the override")
	}
}

// The provider's own maps for the editor, by model id, hold only its models.
func TestModelOverridesForEditor(t *testing.T) {
	setupOverride(t)
	for _, s := range []struct{ ref, ov string }{
		{"relay/sol", `{"temperature":0.9}`},
		{"relay/*", `{"temperature":0.1}`},
		{"other/sol", `{"temperature":0.5}`},
	} {
		if err := SetModelOverride(s.ref, json.RawMessage(s.ov)); err != nil {
			t.Fatal(err)
		}
	}
	if err := SetModelOverrideInherit("relay/luna", true); err != nil {
		t.Fatal(err)
	}
	p, err := Find("relay")
	if err != nil {
		t.Fatal(err)
	}
	got := p.ModelOverrides()
	if len(got) != 2 || !sameJSON(got["sol"], json.RawMessage(`{"temperature":0.9}`)) || !sameJSON(got["*"], json.RawMessage(`{"temperature":0.1}`)) {
		t.Fatalf("ModelOverrides: %v", got)
	}
	if inh := p.ModelOverrideInherits(); len(inh) != 1 || inh["luna"] != false {
		t.Fatalf("ModelOverrideInherits: %v", inh)
	}
}

// A rename moves the override and the opt-out with the provider's other
// per-model values, and a carry keeps them when another page saves.
func TestModelOverrideRenameAndCarry(t *testing.T) {
	setupOverride(t)
	if err := SetModelOverride("relay/sol", json.RawMessage(`{"temperature":0.9}`)); err != nil {
		t.Fatal(err)
	}
	if err := SetModelOverrideInherit("relay/luna", true); err != nil {
		t.Fatal(err)
	}
	s := settings.Load()
	if !s.RenamePerModel("relay", "relay2") {
		t.Fatal("the rename moved nothing")
	}
	if _, ok := s.ModelOverride["relay2/sol"]; !ok {
		t.Fatalf("the override did not move: %v", s.ModelOverride)
	}
	if _, ok := s.ModelOverrideInherit["relay2/luna"]; !ok {
		t.Fatalf("the opt-out did not move: %v", s.ModelOverrideInherit)
	}
	var carried settings.Settings
	settings.CarryPerModel(&carried, &s)
	if _, ok := carried.ModelOverride["relay2/sol"]; !ok {
		t.Fatalf("the carry lost the override: %v", carried.ModelOverride)
	}
	if _, ok := carried.ModelOverrideInherit["relay2/luna"]; !ok {
		t.Fatalf("the carry lost the opt-out: %v", carried.ModelOverrideInherit)
	}
}
