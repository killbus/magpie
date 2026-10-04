package gateway

import (
	"context"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tidwall/gjson"
	"github.com/yetone/magpie/internal/plugin"
	"github.com/yetone/magpie/internal/provider"
)

func TestPluginModelStrip(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("no bun on PATH")
	}
	fresh(t)
	t.Setenv("MAGPIE_BUN", bun)
	t.Setenv("FAKE_ID", "fakeco")
	t.Cleanup(plugin.Settle)
	upstream := &fake{t: t, reply: sse(`data: {"id":"ok","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`, `data: [DONE]`)}
	up := httptest.NewServer(upstream)
	defer up.Close()
	t.Setenv("FAKE_BASE", up.URL+"/v1")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	fixture, _ := filepath.Abs("../plugin/testdata/fake/index.js")
	if _, err := plugin.Add(ctx, fixture); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.Providers(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := plugin.APIKey(ctx, "fakeco", 0, nil, "local-test", plugin.NewAccount); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelStrip("fakeco/fake-1", []string{"reasoning_effort"}); err != nil {
		t.Fatal(err)
	}
	server := New()
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"fakeco/fake-1","messages":[],"reasoning_effort":"high"}`)))
	if recorder.Code != 200 || upstream.calls != 1 || gjson.GetBytes(upstream.got, "reasoning_effort").Exists() {
		t.Fatalf("%d %s, upstream %s", recorder.Code, recorder.Body, upstream.got)
	}
	if upstream.head.Get("Authorization") != "Bearer local-test" {
		t.Fatal("request did not run through plugin fetch")
	}
	if err := provider.SetModelStrip("fakeco/*", []string{"reasoning_effort"}); err != nil {
		t.Fatal(err)
	}
	no := false
	empty := []string{}
	if err := provider.SetStripPreference("fakeco/fake-1", &empty, &no); err != nil {
		t.Fatal(err)
	}
	recorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"fakeco/fake-1","messages":[],"reasoning_effort":"high"}`)))
	if recorder.Code != 200 || !gjson.GetBytes(upstream.got, "reasoning_effort").Exists() {
		t.Fatalf("plugin opt-out: %d %s, upstream %s", recorder.Code, recorder.Body, upstream.got)
	}
}
