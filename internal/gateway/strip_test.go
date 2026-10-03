package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/tidwall/gjson"
	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

func TestModelStripOutgoing(t *testing.T) {
	for _, test := range []struct {
		name, path, body string
		protocol         provider.Protocol
	}{
		{"chat", "/v1/chat/completions", `{"model":"fake/m1","messages":[],"reasoning_effort":"high","reasoning":{"effort":"high","summary":"auto"}}`, provider.Chat},
		{"responses", "/v1/responses", `{"model":"fake/m1","input":"hi","reasoning":{"effort":"high","summary":"auto"}}`, provider.Responses},
		{"anthropic", "/v1/messages", `{"model":"fake/m1","messages":[],"max_tokens":100,"thinking":{"type":"adaptive"},"output_config":{"effort":"high","format":{"type":"text"}}}`, provider.Anthropic},
		{"translated", "/v1/responses", `{"model":"fake/m1","input":"hi","reasoning":{"effort":"high"}}`, provider.Chat},
	} {
		t.Run(test.name, func(t *testing.T) {
			fresh(t)
			upstream := &fake{t: t, ctype: "application/json", reply: `{"id":"ok","choices":[],"output":[],"content":[],"usage":{}}`}
			if test.name == "translated" {
				upstream.ctype = "text/event-stream"
				upstream.reply = sse(`data: {"id":"ok","choices":[{"index":0,"delta":{"content":"ok"},"finish_reason":"stop"}]}`, `data: [DONE]`)
			}
			setup(t, test.protocol, upstream)
			config := settings.Load()
			config.ModelStrip = map[string][]string{"fake/*": {"reasoning_effort"}, "fake/m1": {"reasoning.effort", "output_config.effort"}}
			config.ModelWires = map[string]string{"fake/m1": "renamed"}
			if err := settings.Save(config); err != nil {
				t.Fatal(err)
			}
			server := New()
			recorder := httptest.NewRecorder()
			server.Handler().ServeHTTP(recorder, httptest.NewRequest("POST", test.path, strings.NewReader(test.body)))
			if recorder.Code != http.StatusOK {
				t.Fatalf("%d: %s", recorder.Code, recorder.Body)
			}
			for _, path := range []string{"reasoning_effort", "reasoning.effort", "output_config.effort"} {
				if gjson.GetBytes(upstream.got, path).Exists() {
					t.Errorf("%s still sent: %s", path, upstream.got)
				}
			}
			if modelOf(upstream.got) != "renamed" {
				t.Fatalf("wire name lost: %s", upstream.got)
			}
			if test.name == "responses" && gjson.GetBytes(upstream.got, "reasoning.summary").String() != "auto" {
				t.Fatalf("summary lost: %s", upstream.got)
			}
			if test.name == "anthropic" && !gjson.GetBytes(upstream.got, "output_config.format").Exists() {
				t.Fatalf("output format lost: %s", upstream.got)
			}
			if calls := server.Recent(); len(calls) != 1 || len(calls[0].Stripped) == 0 {
				t.Fatalf("missing stripped annotation: %+v", calls)
			}
		})
	}
}

func TestWithoutPaths(t *testing.T) {
	for _, test := range []struct {
		name, body, want string
		paths            []string
	}{
		{"nested", `{"reasoning":{"effort":"high","summary":"auto"},"id":9007199254740993}`, `{"reasoning":{"summary":"auto"},"id":9007199254740993}`, []string{"reasoning.effort"}},
		{"prune", `{"reasoning":{"effort":"high"},"keep":{}}`, `{"keep":{}}`, []string{"reasoning.effort"}},
		{"deep", `{"a":{"b":{"c":true}}}`, `{}`, []string{"a.b.c"}},
		{"array", `{"messages":[{"role":"assistant","reasoning_content":"secret"},{"reasoning_content":"secret"},null,3]}`, `{"messages":[{"role":"assistant"},{},null,3]}`, []string{"messages[].reasoning_content"}},
		{"arrays", `{"a":[{"b":[{"c":{"d":null}},{"e":true}]}]}`, `{"a":[{"b":[{},{"e":true}]}]}`, []string{"a[].b[].c.d"}},
		{"no-match", "{ \"reasoning\": {}, \"messages\": null }", "{ \"reasoning\": {}, \"messages\": null }", []string{"reasoning.effort", "messages[].field"}},
		{"invalid", `{"reasoning_effort":`, `{"reasoning_effort":`, []string{"reasoning_effort"}},
		{"trailing", `{"reasoning_effort":"high"} {}`, `{"reasoning_effort":"high"} {}`, []string{"reasoning_effort"}},
		{"escaped", `{"reasoning_\u0065ffort":"high"}`, `{}`, []string{"reasoning_effort"}},
		{"invalid-path", `{"model":"keep","a":1}`, `{"model":"keep","a":1}`, []string{"model", "", "a[]"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, removed := withoutPaths([]byte(test.body), test.paths...)
			if test.want == test.body {
				if string(got) != test.body || len(removed) > 0 {
					t.Fatalf("no-op changed body: %s %v", got, removed)
				}
				return
			}
			var actual, expected any
			decoder := json.NewDecoder(strings.NewReader(string(got)))
			decoder.UseNumber()
			if err := decoder.Decode(&actual); err != nil {
				t.Fatal(err)
			}
			decoder = json.NewDecoder(strings.NewReader(test.want))
			decoder.UseNumber()
			decoder.Decode(&expected)
			actualJSON, _ := json.Marshal(actual)
			expectedJSON, _ := json.Marshal(expected)
			if string(actualJSON) != string(expectedJSON) || len(removed) == 0 {
				t.Fatalf("got %s (%v), want %s", got, removed, test.want)
			}
		})
	}
}

func TestModelStripGroupFallback(t *testing.T) {
	fresh(t)
	first := &fake{t: t, ctype: "application/json", code: 429, reply: `{"error":{"message":"rate limited"}}`}
	second := &fake{t: t, ctype: "application/json", reply: `{"id":"ok","choices":[]}`}
	serveOn(t, "first", "one", []string{"m"}, first)
	serveOn(t, "second", "two", []string{"m"}, second)
	if err := provider.SaveGroup(provider.Group{Name: "Strip", Members: []string{"first/m:high", "second/m:high"}, Routing: provider.Ordered}); err != nil {
		t.Fatal(err)
	}
	config := settings.Load()
	config.ModelStrip = map[string][]string{"first/m": {"reasoning_effort"}}
	if err := settings.Save(config); err != nil {
		t.Fatal(err)
	}
	server := New()
	code, reply := postAs(t, server, "strip-session", `{"model":"group/strip","messages":[],"reasoning_effort":"low"}`)
	if code != 200 || gjson.GetBytes(first.got, "reasoning_effort").Exists() || gjson.GetBytes(second.got, "reasoning_effort").String() != "high" {
		t.Fatalf("%d %s; first %s; second %s", code, reply, first.got, second.got)
	}
	if got := server.Recent()[0].Stripped; len(got) != 0 {
		t.Fatalf("fallback kept another model's annotation: %v", got)
	}
	routes := server.Trace(context.Background(), 0, 0).Routes
	if len(routes) != 1 || routes[0].Effort != "low" || len(routes[0].Tries) != 2 || !slices.Equal(routes[0].Tries[0].Stripped, []string{"reasoning_effort"}) || len(routes[0].Tries[1].Stripped) != 0 {
		t.Fatalf("incorrect trace: %+v", routes)
	}
	first.code = 0
	postAs(t, server, "", `{"model":"first/m","messages":[],"reasoning_effort":"high"}`)
	if gjson.GetBytes(first.got, "reasoning_effort").Exists() {
		t.Fatal("direct route bypassed strip policy")
	}
}

func TestModelStripRetryAndIsolation(t *testing.T) {
	fresh(t)
	var bodies [][]byte
	upstream := &fake{t: t, ctype: "application/json", reply: `{"id":"ok","choices":[]}`}
	upstream.refuse = func(body []byte) (int, string) {
		bodies = append(bodies, slices.Clone(body))
		if len(bodies) == 1 {
			return 400, `{"error":{"message":"Function tools with reasoning_effort are not supported; set reasoning_effort to 'none'."}}`
		}
		return 0, ""
	}
	setup(t, provider.Chat, upstream)
	config := settings.Load()
	config.ModelStrip = map[string][]string{"fake/m1": {"reasoning_effort"}}
	if err := settings.Save(config); err != nil {
		t.Fatal(err)
	}
	code, reply := post(t, "/v1/chat/completions", `{"model":"fake/m1","messages":[],"reasoning_effort":"high"}`)
	if code != 200 || len(bodies) != 2 {
		t.Fatalf("%d %s, %d attempts", code, reply, len(bodies))
	}
	for _, body := range bodies {
		if gjson.GetBytes(body, "reasoning_effort").Exists() {
			t.Fatalf("retry reintroduced effort: %s", body)
		}
	}
	config.ModelStrip = nil
	if err := settings.Save(config); err != nil {
		t.Fatal(err)
	}
	post(t, "/v1/chat/completions", `{"model":"fake/m1","messages":[],"reasoning_effort":"high"}`)
	var sent map[string]any
	json.Unmarshal(upstream.got, &sent)
	if sent["reasoning_effort"] != "high" {
		t.Fatalf("reset did not restore effort: %s", upstream.got)
	}
}

func TestModelStripSiblingUnaffected(t *testing.T) {
	fresh(t)
	upstream := &fake{t: t, ctype: "application/json", reply: `{"id":"ok","choices":[]}`}
	serveOn(t, "relay", "key", []string{"one", "two"}, upstream)
	if err := provider.SetModelStrip("relay/one", []string{"reasoning_effort"}); err != nil {
		t.Fatal(err)
	}
	server := New()
	for _, model := range []string{"one", "two", "one"} {
		code, reply := postAs(t, server, "", `{"model":"relay/`+model+`","messages":[],"reasoning_effort":"high"}`)
		if code != 200 || gjson.GetBytes(upstream.got, "reasoning_effort").Exists() != (model == "two") {
			t.Fatalf("%d %s, %s: %s", code, reply, model, upstream.got)
		}
	}
}

func TestModelStripTranslatedProtocols(t *testing.T) {
	for _, protocol := range []provider.Protocol{provider.Chat, provider.Responses, provider.Anthropic} {
		t.Run(string(protocol), func(t *testing.T) {
			fresh(t)
			upstream := &fake{t: t, ctype: "application/json", reply: `{}`}
			setup(t, protocol, upstream)
			config := settings.Load()
			config.ModelStrip = map[string][]string{"fake/m1": {"reasoning_effort", "reasoning.effort", "output_config.effort"}}
			if err := settings.Save(config); err != nil {
				t.Fatal(err)
			}
			current, err := provider.Find("fake")
			if err != nil {
				t.Fatal(err)
			}
			request := &Request{Model: "m1", Effort: "high", MaxTokens: 4096}
			response, _, err := New().forwardTranslated(context.Background(), *current, protocol, request, "m1", nil)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			for _, path := range config.ModelStrip["fake/m1"] {
				if gjson.GetBytes(upstream.got, path).Exists() {
					t.Errorf("%s survived encoding: %s", path, upstream.got)
				}
			}
		})
	}
}

func TestModelStripCountTokens(t *testing.T) {
	fresh(t)
	upstream := &fake{t: t, ctype: "application/json", reply: `{"input_tokens":12}`}
	setup(t, provider.Anthropic, upstream)
	if err := provider.SetModelStrip("fake/m1", []string{"output_config.effort"}); err != nil {
		t.Fatal(err)
	}
	code, reply := post(t, "/v1/messages/count_tokens", `{"model":"fake/m1","messages":[{"role":"user","content":"hi"}],"output_config":{"effort":"high"}}`)
	if code != 200 || upstream.calls != 1 || gjson.GetBytes(upstream.got, "output_config").Exists() {
		t.Fatalf("%d %s: %s", code, reply, upstream.got)
	}
}
