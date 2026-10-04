package gui

import (
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

// A provider's Save carries what its editor's Names & levels changed
// (modelPrefs), and makes it with the rest (ARNO on Discord: each level
// ticked was applied on its own, before the Save).
func TestProviderSaveModelPrefs(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(body)))
		return w
	}
	if w := post(`{"id":"relay","name":"Relay","key":"k","chat":"http://127.0.0.1:1/v1","models":["sol"],"new":true}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if w := post(`{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol"],
		"modelPrefs":{"sol":{"name":"My Sol","efforts":["low","high"],"images":true}}}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	s := settings.Load()
	if s.ModelNames["relay/sol"] != "My Sol" || !slices.Equal(s.ModelEfforts["relay/sol"], []string{"low", "high"}) || !s.ModelImages["relay/sol"] {
		t.Fatalf("names %v, efforts %v, images %v", s.ModelNames, s.ModelEfforts, s.ModelImages)
	}
	// the model it is the same as, for the groups magpie finds (#583)
	if w := post(`{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol"],"modelPrefs":{"sol":{"same":"deepseek-v4.1-flash"}}}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if got := settings.Load().ModelSameAs["relay/sol"]; got != "deepseek-v4.1-flash" {
		t.Fatalf("same as %q", got)
	}
	// one it refuses fails the Save, saying why
	if w := post(`{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol"],"modelPrefs":{"sol":{"strip":["reasoning_effort","reasoning.effort","output_config.effort"]}}}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if got := settings.Load().ModelStrip["relay/sol"]; !slices.Equal(got, []string{"reasoning_effort", "reasoning.effort", "output_config.effort"}) {
		t.Fatal(got)
	}
	if w := post(`{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol"],"modelPrefs":{"sol":{"strip":["bad..path"]}}}`); w.Code == 200 {
		t.Fatal("invalid strip path accepted")
	}
	if w := post(`{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol"],"modelPrefs":{"sol":{"strip":[]}}}`); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(settings.Load().ModelStrip) != 0 {
		t.Fatal("strip reset was not saved")
	}
	if w := post(`{"id":"relay","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol"],"modelPrefs":{"sol":{"efforts":["loud"]}}}`); w.Code == 200 || !strings.Contains(w.Body.String(), "loud") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestProviderSaveStrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	mux := http.NewServeMux()
	providerRoutes(mux, nil)
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/provider/save", strings.NewReader(body)))
		return w
	}
	if w := post(`{"id":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":[],"new":true,"strip":["metadata"]}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	p, _ := provider.Find("relay")
	j := providerInfo(*p, nil)
	if !j.StripSupported || !slices.Equal(j.Strip, []string{"metadata"}) {
		t.Fatal("empty model list lost provider strip", j.Strip)
	}
	if w := post(`{"id":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol"],"modelPrefs":{"sol":{"strip":[],"inheritStrip":false}}}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if s := settings.Load(); provider.StripInheritsIn(s, "relay", "sol") || len(provider.StripPathsIn(s, "relay", "sol")) != 0 {
		t.Fatal("opt-out not saved")
	}
	p, _ = provider.Find("relay")
	j = providerInfo(*p, nil)
	found := false
	for _, model := range j.Models {
		if model.ID == "sol" {
			found = true
			if model.InheritStrip || !slices.Equal(model.StripAll, []string{"metadata"}) {
				t.Fatal("model response lost explicit opt-out or provider rules")
			}
		}
	}
	if !found {
		t.Fatal("saved model absent from response")
	}
	if w := post(`{"id":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol"],"strip":null,"modelPrefs":{"sol":{"strip":null,"inheritStrip":null}}}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if s := settings.Load(); provider.StripInheritsIn(s, "relay", "sol") || !slices.Equal(s.ModelStrip["relay/*"], []string{"metadata"}) {
		t.Fatal("null fields changed preferences")
	}
	if w := post(`{"id":"relay","name":"Changed","chat":"http://127.0.0.1:1/v1","strip":["bad..path"],"modelPrefs":{"sol":{"inheritStrip":true}}}`); w.Code == 200 {
		t.Fatal("bad path accepted")
	}
	p, _ = provider.Find("relay")
	if p.Name != "Relay" || provider.StripInheritsIn(settings.Load(), "relay", "sol") {
		t.Fatal("invalid save partially applied")
	}
	if w := post(`{"id":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol"]}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if provider.StripInheritsIn(settings.Load(), "relay", "sol") {
		t.Fatal("omitted field reset inheritance")
	}
	if w := post(`{"id":"renamed","from":"relay","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol"]}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if s := settings.Load(); provider.StripInheritsIn(s, "renamed", "sol") || !slices.Equal(s.ModelStrip["renamed/*"], []string{"metadata"}) {
		t.Fatal("rename lost rules")
	}
	if w := post(`{"id":"renamed","name":"Relay","chat":"http://127.0.0.1:1/v1","models":["sol"],"strip":[],"modelPrefs":{"sol":{"inheritStrip":true}}}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body)
	}
	if s := settings.Load(); len(s.ModelStrip) != 0 || len(s.ModelStripInherit) != 0 {
		t.Fatal("reset did not clear preferences")
	}
}
