package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

func overrideGUI(t *testing.T) http.Handler {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save",
		strings.NewReader(`{"id":"relay","name":"Relay","key":"k","chat":"http://127.0.0.1:1/v1","models":["sol","luna","mars","venus"],"new":true}`)))
	if w.Code != 200 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	// a provider with no override at all, for the empty case
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save",
		strings.NewReader(`{"id":"box","name":"Box","key":"k","chat":"http://127.0.0.1:2/v1","models":["rock"],"new":true}`)))
	if w.Code != 200 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	return mux
}

// The provider editor's Param override entries save through the provider's
// Save: a model's own (its chip's menu), the provider's <pid>/* (Advanced),
// the opt-out, and clearing one.
func TestProviderSaveModelOverride(t *testing.T) {
	mux := overrideGUI(t)
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(body)))
		return w
	}
	if w := post(`{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol","luna","mars","venus"],
		"modelPrefs":{"sol":{"override":{"temperature":0.2}},"*":{"override":{"temperature":0.1}},"mars":{"overrideOff":true}}}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	s := settings.Load()
	if !strings.Contains(string(s.ModelOverride["relay/sol"]), "0.2") {
		t.Errorf("the model's own: %s", s.ModelOverride["relay/sol"])
	}
	if !strings.Contains(string(s.ModelOverride["relay/*"]), "0.1") {
		t.Errorf("the provider's: %s", s.ModelOverride["relay/*"])
	}
	if v, ok := s.ModelOverrideInherit["relay/mars"]; !ok || v {
		t.Errorf("the opt-out: %v %v", v, ok)
	}
	// an override that targets the root model is refused, naming why
	if w := post(`{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol","luna","mars","venus"],
		"modelPrefs":{"sol":{"override":{"model":"x"}}}}`); w.Code == 200 {
		t.Error("a root-model target was accepted")
	}
	// an empty box takes the model's own away, ClearOverride says so
	if w := post(`{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol","luna","mars","venus"],
		"modelPrefs":{"sol":{"clearOverride":true}}}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if _, ok := settings.Load().ModelOverride["relay/sol"]; ok {
		t.Error("clearOverride left it behind")
	}
}

// The chip's badge and the editors read where each model's override comes
// from: its own, its provider's <pid>/*, or — the opt-out and no own one —
// none at all. The provider's own is given back for the editor's box.
func TestProviderJSONShowsModelOverride(t *testing.T) {
	mux := overrideGUI(t)
	for _, c := range []struct {
		ref string
		raw string
	}{
		{"relay/*", `{"temperature":0.1}`},
		{"relay/sol", `{"temperature":0.2}`},
	} {
		if err := provider.SetModelOverride(c.ref, json.RawMessage(c.raw)); err != nil {
			t.Fatalf("%s: %v", c.ref, err)
		}
	}
	if err := provider.SetModelOverrideInherit("relay/mars", true); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/providers", nil))
	if w.Code != 200 {
		t.Fatalf("GET providers: %d %s", w.Code, w.Body)
	}
	var got struct {
		Providers []struct {
			ID       string          `json:"id"`
			Override json.RawMessage `json:"override"`
			Models   []struct {
				ID           string          `json:"id"`
				Override     json.RawMessage `json:"override"`
				OverrideFrom string          `json:"overrideFrom"`
				OverrideOff  bool            `json:"overrideOff"`
			} `json:"models"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v (%s)", err, w.Body)
	}
	seen, providerRaw := map[string]string{}, ""
	for _, p := range got.Providers {
		if p.ID == "relay" {
			providerRaw = string(p.Override)
		}
		for _, m := range p.Models {
			seen[m.ID] = m.OverrideFrom
			if len(m.Override) > 0 {
				seen[m.ID] += ":" + string(m.Override)
			}
			if m.OverrideOff {
				seen[m.ID] += "+off"
			}
		}
	}
	for id, want := range map[string]string{
		"sol":   `own:{"temperature":0.2}`,
		"luna":  "provider",
		"mars":  "+off",
		"venus": "provider",
		"rock":  "",
	} {
		if seen[id] != want {
			t.Errorf("%s: %q, want %q (all: %v)", id, seen[id], want, seen)
		}
	}
	if !strings.Contains(providerRaw, "0.1") {
		t.Errorf("the provider's own not given: %q", providerRaw)
	}
}
