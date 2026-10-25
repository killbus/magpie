package gui

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/settings"
)

// The editor's one Param Override field saves the JSON through the provider's
// Save (modelPrefs), and the provider's JSON shows it back, saying whether it
// is the model's own or its provider's, and whether the model opted out.

func overrideGUI(t *testing.T) http.Handler {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(body)))
		return w
	}
	if w := post(`{"id":"relay","name":"Relay","key":"k","chat":"http://127.0.0.1:1/v1","models":["sol","luna"],"new":true}`); w.Code != 200 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	return mux
}

func TestProviderSaveModelOverride(t *testing.T) {
	mux := overrideGUI(t)
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(body)))
		return w
	}
	// save one through the editor's field
	if w := post(`{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol","luna"],
		"modelPrefs":{"sol":{"override":{"temperature":0.2,"operations":[{"path":"max_tokens","mode":"set","value":8000}]}}}}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	s := settings.Load()
	if len(s.ModelOverride["relay/sol"]) == 0 || !strings.Contains(string(s.ModelOverride["relay/sol"]), "temperature") {
		t.Fatalf("not saved: %v", s.ModelOverride)
	}
	// an invalid override is refused, naming why
	if w := post(`{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol","luna"],
		"modelPrefs":{"sol":{"override":{"operations":[{"path":"model","mode":"set","value":"x"}]}}}}`); w.Code == 200 {
		t.Fatal("a root-model target was accepted")
	}
	// a provider-wide one, and the model shown to inherit it
	if err := post(`{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol","luna"],
		"modelPrefs":{"*":{"override":{"temperature":0.1}}}}`); err.Code != 200 {
		t.Fatalf("%d %s", err.Code, err.Body)
	}
	// the opt-out
	if w := post(`{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol","luna"],
		"modelPrefs":{"luna":{"overrideOff":true}}}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if v, ok := settings.Load().ModelOverrideInherit["relay/luna"]; !ok || v {
		t.Fatalf("the opt-out: %v %v", v, ok)
	}
	// clearOverride takes it away
	if w := post(`{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol","luna"],
		"modelPrefs":{"sol":{"clearOverride":true}}}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if _, ok := settings.Load().ModelOverride["relay/sol"]; ok {
		t.Fatal("clearOverride left it behind")
	}
}

// The provider's JSON says where each model's override came from, so the
// editor can show the model's own, its provider's, or none.
func TestProviderJSONShowsModelOverride(t *testing.T) {
	mux := overrideGUI(t)
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(body)))
		return w
	}
	if w := post(`{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol","luna"],
		"modelPrefs":{"sol":{"override":{"temperature":0.2}},"*":{"override":{"temperature":0.1}},"luna":{"overrideOff":true}}}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/api/providers", nil))
	if w.Code != 200 {
		t.Fatalf("GET providers: %d %s", w.Code, w.Body)
	}
	var got struct {
		Providers []struct {
			ID     string `json:"id"`
			Models []struct {
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
	byModel := map[string]struct {
		from string
		raw  json.RawMessage
		off  bool
	}{}
	for _, p := range got.Providers {
		if p.ID != "relay" {
			continue
		}
		for _, m := range p.Models {
			byModel[m.ID] = struct {
				from string
				raw  json.RawMessage
				off  bool
			}{m.OverrideFrom, m.Override, m.OverrideOff}
		}
	}
	if v := byModel["sol"]; v.from != "own" || len(v.raw) == 0 {
		t.Errorf("sol: from %q raw %s, want its own", v.from, v.raw)
	}
	if v := byModel["luna"]; v.from != "provider" || !v.off {
		t.Errorf("luna: from %q off %v, want the provider's and opted out", v.from, v.off)
	}
}
