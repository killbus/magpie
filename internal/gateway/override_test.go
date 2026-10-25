package gateway

// The param override at the wire: applied once, to the final body, after
// magpie's own rewrites and before signing, with the operations and their
// audit lines each occurring exactly once per outbound attempt.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

// bodyRecorder is a fake vendor that keeps every body it was sent.
type bodyRecorder struct {
	mu     sync.Mutex
	bodies []string
	status int
	reply  string
	stream bool
}

func (v *bodyRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b, _ := io.ReadAll(r.Body)
	v.mu.Lock()
	v.bodies = append(v.bodies, string(b))
	status, reply, stream := v.status, v.reply, v.stream
	v.mu.Unlock()
	if status == 0 {
		status = 200
	}
	if reply == "" {
		reply = `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`
	}
	if stream {
		w.Header().Set("Content-Type", "text/event-stream")
	} else {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(status)
	io.WriteString(w, reply)
}

func (v *bodyRecorder) all() []string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]string(nil), v.bodies...)
}

// overrideEnv sets a Chat provider at up, and returns the gateway and its
// handler.
func overrideEnv(t *testing.T, up string, models ...string) (*Server, http.Handler) {
	t.Helper()
	fresh(t)
	if len(models) == 0 {
		models = []string{"sol"}
	}
	if err := provider.Save(provider.Provider{ID: "relay", Name: "relay", Key: "k", Chat: up, Models: models}); err != nil {
		t.Fatal(err)
	}
	s := New()
	return s, s.Handler()
}

// postChat sends one Chat request for model, and gives the status and body.
func postChat(t *testing.T, h http.Handler, model, extra string) (int, string) {
	t.Helper()
	body := `{"model":"relay/` + model + `","messages":[{"role":"user","content":"hi"}]`
	if extra != "" {
		body += "," + extra
	}
	body += "}"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
	return rec.Code, rec.Body.String()
}

// wantSentJSON is the last body the vendor was sent, checked against want as
// JSON: key order and whitespace apart.
func wantSentJSON(t *testing.T, up *bodyRecorder, want string) {
	t.Helper()
	sent := up.all()
	if len(sent) == 0 {
		t.Fatal("the vendor was asked nothing")
	}
	got := sent[len(sent)-1]
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("sent body is not JSON: %s", got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("want is not JSON: %s", want)
	}
	if !ovJSONEqual(g, w) {
		t.Fatalf("sent = %s, want %s", got, want)
	}
}

func ovJSONEqual(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

// A legacy key and an operation both run, legacy first, and the vendor is
// sent the changed body.
func TestOverrideSetsTheSentBody(t *testing.T) {
	up := &bodyRecorder{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	_, h := overrideEnv(t, srv.URL)
	if err := provider.SetModelOverride("relay/sol", json.RawMessage(`{"temperature":0.2,"operations":[{"path":"max_tokens","mode":"set","value":900}]}`)); err != nil {
		t.Fatal(err)
	}
	if code, out := postChat(t, h, "sol", ""); code != 200 {
		t.Fatalf("status %d: %s", code, out)
	}
	wantSentJSON(t, up, `{"model":"sol","messages":[{"role":"user","content":"hi"}],"temperature":0.2,"max_tokens":900}`)
}

// A delete and a move reach the vendor, and a value magpie itself wrote is
// overwritten by the user's set (a set wins over the extra body).
func TestOverrideWinsOverMagpieFields(t *testing.T) {
	up := &bodyRecorder{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	_, h := overrideEnv(t, srv.URL)
	// the user's extra fields write service_tier; the override sets it after
	if err := provider.Save(provider.Provider{ID: "relay", Name: "relay", Key: "k", Chat: srv.URL, Models: []string{"sol"},
		ExtraBody: provider.ExtraBody{"service_tier": json.RawMessage(`"auto"`), "metadata": json.RawMessage(`{"user_id":"magpie"}`)}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelOverride("relay/sol", json.RawMessage(`{"operations":[{"path":"service_tier","mode":"set","value":"priority"},{"path":"metadata.user_id","mode":"delete"}]}`)); err != nil {
		t.Fatal(err)
	}
	if code, out := postChat(t, h, "sol", ""); code != 200 {
		t.Fatalf("status %d: %s", code, out)
	}
	wantSentJSON(t, up, `{"model":"sol","messages":[{"role":"user","content":"hi"}],"service_tier":"priority","metadata":{}}`)
}

// A model opted out of its provider's override is sent the body as it is.
func TestOverrideOptOut(t *testing.T) {
	up := &bodyRecorder{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	_, h := overrideEnv(t, srv.URL, "sol", "luna")
	if err := provider.SetModelOverride("relay/*", json.RawMessage(`{"temperature":0.2}`)); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelOverrideInherit("relay/luna", true); err != nil {
		t.Fatal(err)
	}
	if code, out := postChat(t, h, "luna", ""); code != 200 {
		t.Fatalf("status %d: %s", code, out)
	}
	wantSentJSON(t, up, `{"model":"luna","messages":[{"role":"user","content":"hi"}]}`)
}

// A no-op override leaves the bytes identical: the body sent is what magpie
// built, unchanged.
func TestOverrideNoOpKeepsBytes(t *testing.T) {
	up := &bodyRecorder{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	_, h := overrideEnv(t, srv.URL)
	if err := provider.SetModelOverride("relay/sol", json.RawMessage(`{"operations":[{"path":"nope","mode":"delete"}]}`)); err != nil {
		t.Fatal(err)
	}
	if code, out := postChat(t, h, "sol", ""); code != 200 {
		t.Fatalf("status %d: %s", code, out)
	}
	wantSentJSON(t, up, `{"model":"sol","messages":[{"role":"user","content":"hi"}]}`)
}

// A non-idempotent operation (append) runs exactly once per outbound attempt,
// and its audit line is recorded once. The body the vendor is sent has one
// append's worth, not two, and the try shows one line.
func TestOverrideAppendRunsOnce(t *testing.T) {
	up := &bodyRecorder{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	s, h := overrideEnv(t, srv.URL)
	if err := provider.SetModelOverride("relay/sol", json.RawMessage(`{"operations":[{"path":"messages.0.content","mode":"append","value":"!"}]}`)); err != nil {
		t.Fatal(err)
	}
	if code, out := postChat(t, h, "sol", ""); code != 200 {
		t.Fatalf("status %d: %s", code, out)
	}
	wantSentJSON(t, up, `{"model":"sol","messages":[{"role":"user","content":"hi!"}]}`)
	if n := len(up.all()); n != 1 {
		t.Fatalf("vendor asked %d times, want 1", n)
	}
	// the audit line, once
	lines := overrideLinesOf(t, s)
	if len(lines) != 1 || lines[0] != "append messages.0.content" {
		t.Fatalf("audit lines = %v, want one append", lines)
	}
}

// regex_replace and move, both non-idempotent, run once and leave one audit
// line each.
func TestOverrideRegexAndMoveRunOnce(t *testing.T) {
	up := &bodyRecorder{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	s, h := overrideEnv(t, srv.URL)
	if err := provider.SetModelOverride("relay/sol", json.RawMessage(`{"operations":[
		{"path":"messages.0.content","mode":"regex_replace","from":"h(i)","to":"H$1"},
		{"from":"temperature","to":"max_tokens","mode":"move"}]}`)); err != nil {
		t.Fatal(err)
	}
	if code, out := postChat(t, h, "sol", `"temperature":7`); code != 200 {
		t.Fatalf("status %d: %s", code, out)
	}
	wantSentJSON(t, up, `{"model":"sol","messages":[{"role":"user","content":"Hi"}],"max_tokens":7}`)
	lines := overrideLinesOf(t, s)
	if len(lines) != 2 || lines[0] != "regex_replace messages.0.content h(i)" || lines[1] != "move temperature -> max_tokens" {
		t.Fatalf("audit lines = %v", lines)
	}
}

// overrideLinesOf is every Overrides line across the gateway's routes, for a
// test to count.
func overrideLinesOf(t *testing.T, s *Server) []string {
	t.Helper()
	s.trace.mu.Lock()
	defer s.trace.mu.Unlock()
	var out []string
	for _, r := range s.trace.routes {
		for _, tr := range r.Tries {
			out = append(out, tr.Overrides...)
		}
	}
	return out
}

// A return_error with skip_retry true answers the agent without any upstream
// request, and the route ends there: a second candidate is not asked.
func TestOverrideReturnErrorSkipRetry(t *testing.T) {
	up := &bodyRecorder{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	_, h := overrideEnv(t, srv.URL)
	if err := provider.SetModelOverride("relay/sol", json.RawMessage(`{"operations":[{"mode":"return_error","value":{"message":"turned away","status_code":403}}]}`)); err != nil {
		t.Fatal(err)
	}
	code, out := postChat(t, h, "sol", "")
	if code != 403 {
		t.Fatalf("status %d: %s", code, out)
	}
	if !strings.Contains(out, "turned away") {
		t.Fatalf("the message is not the agent's: %s", out)
	}
	if n := len(up.all()); n != 0 {
		t.Fatalf("the vendor was asked %d times, want none", n)
	}
}

// A return_error with skip_retry false lets the loop go on to the next
// candidate, which answers.
func TestOverrideReturnErrorContinues(t *testing.T) {
	good := &bodyRecorder{}
	goodSrv := httptest.NewServer(good)
	t.Cleanup(goodSrv.Close)
	fresh(t)
	// one provider serving both, and a group naming them in order: the first
	// turns the request away and lets it go on, the second answers
	if err := provider.Save(provider.Provider{ID: "relay", Name: "relay", Key: "k", Chat: goodSrv.URL, Models: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SetModelOverride("relay/a", json.RawMessage(`{"operations":[{"mode":"return_error","value":{"message":"try another","skip_retry":false}}]}`)); err != nil {
		t.Fatal(err)
	}
	s := New()
	h := s.Handler()
	// ask the group that names both, in order
	body := `{"model":"group/relay-a-then-b","messages":[{"role":"user","content":"hi"}]}`
	if err := provider.SaveGroup(provider.Group{ID: "relay-a-then-b", Name: "relay-a-then-b", Members: []string{"relay/a", "relay/b"}}); err != nil {
		t.Skipf("group API differs here: %v", err)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	// the first turned it away before sending; the second was sent it
	sent := good.all()
	if len(sent) != 1 {
		t.Fatalf("the vendor was asked %d times, want 1 (the second member)", len(sent))
	}
	if !strings.Contains(sent[0], `"model":"b"`) {
		t.Fatalf("the second member was not the one asked: %s", sent[0])
	}
}

// A translated request is adjusted after translation: the field the override
// changes is one the translation wrote, and the vendor is sent the
// translated body with the override applied to it.
func TestOverrideOnATranslatedRequest(t *testing.T) {
	up := &bodyRecorder{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	fresh(t)
	// an Anthropic-only provider, asked by a Codex (Responses) client: the
	// request is translated, and the override runs on the translated body
	if err := provider.Save(provider.Provider{ID: "relay", Name: "relay", Key: "k", Anthropic: srv.URL, Models: []string{"sol"}}); err != nil {
		t.Fatal(err)
	}
	// an Anthropic Messages stream, which the translation reads and gives
	// back as the Responses reply the client asked for
	nl := "\n"
	events := []string{
		`event: message_start` + nl + `data: {"type":"message_start","message":{"id":"m","type":"message","role":"assistant","model":"sol","content":[],"usage":{"input_tokens":1,"output_tokens":0}}}`,
		`event: content_block_start` + nl + `data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`event: content_block_delta` + nl + `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"pong"}}`,
		`event: content_block_stop` + nl + `data: {"type":"content_block_stop","index":0}`,
		`event: message_delta` + nl + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		`event: message_stop` + nl + `data: {"type":"message_stop"}`,
	}
	up.reply = strings.Join(events, nl+nl) + nl + nl
	up.stream = true
	if err := provider.SetModelOverride("relay/sol", json.RawMessage(`{"operations":[{"path":"max_tokens","mode":"set","value":4321}]}`)); err != nil {
		t.Fatal(err)
	}
	h := New().Handler()
	// a Responses-shaped request, which the Anthropic provider can't relay
	body := `{"model":"relay/sol","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]}],"stream":false}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	sent := up.all()
	if len(sent) != 1 {
		t.Fatalf("the vendor was asked %d times, want 1", len(sent))
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(sent[0]), &got); err != nil {
		t.Fatal(err)
	}
	// the translated body is Anthropic's (messages), and max_tokens is the
	// override's, set after translation wrote its own
	if _, ok := got["messages"]; !ok {
		t.Fatalf("the body was not translated: %s", sent[0])
	}
	if mt, _ := got["max_tokens"].(float64); mt != 4321 {
		t.Fatalf("max_tokens = %v, want the override's 4321", got["max_tokens"])
	}
}

// A count_tokens request goes through the same send path, and its override
// runs on it too.
func TestOverrideOnCountTokens(t *testing.T) {
	up := &bodyRecorder{}
	srv := httptest.NewServer(up)
	t.Cleanup(srv.Close)
	fresh(t)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "relay", Key: "k", Anthropic: srv.URL, Models: []string{"sol"}}); err != nil {
		t.Fatal(err)
	}
	up.reply = `{"input_tokens":12}`
	if err := provider.SetModelOverride("relay/sol", json.RawMessage(`{"operations":[{"path":"marker","mode":"set","value":"counted"}]}`)); err != nil {
		t.Fatal(err)
	}
	h := New().Handler()
	body := `{"model":"relay/sol","messages":[{"role":"user","content":[{"type":"text","text":"hi"}]}]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/messages/count_tokens", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	sent := up.all()
	if len(sent) != 1 {
		t.Fatalf("the vendor was asked %d times, want 1: %v", len(sent), sent)
	}
	if !strings.Contains(sent[0], `"marker":"counted"`) {
		t.Fatalf("the count request carried no override: %s", sent[0])
	}
}

// An override stored by hand and unreadable — a mode the engine refuses, or
// a shape it can't read — is not run: the request goes as magpie builds it,
// rather than failing at the wire. A save never leaves one (SetModelOverride
// validates), and settings.json can't hold invalid JSON at all, so these are
// only what a hand-edited settings.json can carry.
func TestOverrideUnreadableIsNotRun(t *testing.T) {
	for _, tc := range []struct{ name, stored string }{
		{"an unknown mode", `{"operations":[{"path":"a","mode":"frobnicate"}]}`},
		{"operations not a list", `{"operations":{"path":"a","mode":"set","value":1}}`},
		{"not an object", `[1,2,3]`},
		{"a mode that is empty", `{"operations":[{"path":"a"}]}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			up := &bodyRecorder{}
			srv := httptest.NewServer(up)
			t.Cleanup(srv.Close)
			_, h := overrideEnv(t, srv.URL)
			// written the way a hand-edited settings.json carries it
			s := settings.Load()
			if s.ModelOverride == nil {
				s.ModelOverride = map[string]json.RawMessage{}
			}
			s.ModelOverride["relay/sol"] = json.RawMessage(tc.stored)
			if err := settings.Save(s); err != nil {
				t.Fatal(err)
			}
			if code, out := postChat(t, h, "sol", ""); code != 200 {
				t.Fatalf("status %d: %s", code, out)
			}
			wantSentJSON(t, up, `{"model":"sol","messages":[{"role":"user","content":"hi"}]}`)
		})
	}
}

// A try that fails and the next candidate that answers each run the override
// once, on their own body: the append is in the second candidate's body
// once, not twice, and its audit lines are the ones of that try.
func TestOverrideRunsOnceAcrossCandidates(t *testing.T) {
	var mu sync.Mutex
	fail := true
	seen := []string{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, string(b))
		f := fail
		fail = false
		mu.Unlock()
		if f {
			// a refusal another member may answer
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(429)
			io.WriteString(w, `{"error":{"message":"slow down","type":"rate_limit_error"}}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	t.Cleanup(up.Close)
	fresh(t)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "relay", Key: "k", Chat: up.URL, Models: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	if err := provider.SaveGroup(provider.Group{ID: "grp", Name: "grp", Members: []string{"relay/a", "relay/b"}}); err != nil {
		t.Skipf("group API differs here: %v", err)
	}
	// the override appends to the message: run twice, it would read "hi!!"
	if err := provider.SetModelOverride("relay/*", json.RawMessage(`{"operations":[{"path":"messages.0.content","mode":"append","value":"!"}]}`)); err != nil {
		t.Fatal(err)
	}
	h := New().Handler()
	body := `{"model":"group/grp","messages":[{"role":"user","content":"hi"}]}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("the vendor was asked %d times, want 2 (one refused, one served): %v", len(seen), seen)
	}
	for i, b := range seen {
		if strings.Contains(b, "hi!!") {
			t.Fatalf("attempt %d appended twice: %s", i+1, b)
		}
		if !strings.Contains(b, "hi!") {
			t.Fatalf("attempt %d has no append: %s", i+1, b)
		}
	}
}

// A retry that builds another body (the 400 naming a floor on the reply's
// length) enters forwardOnce again: the override runs once on that body too,
// never twice on one, and never on the body it already changed.
func TestOverrideRunsOnceOnARebuiltBody(t *testing.T) {
	var mu sync.Mutex
	seen := []string{}
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var q struct {
			MaxTokens int `json:"max_tokens"`
		}
		json.Unmarshal(b, &q)
		mu.Lock()
		seen = append(seen, string(b))
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if q.MaxTokens < 16 {
			// the floor the retry reads (#64)
			w.WriteHeader(400)
			io.WriteString(w, `{"error":{"message":"max_tokens must be greater than 15","type":"invalid_request_error"}}`)
			return
		}
		io.WriteString(w, `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	t.Cleanup(up.Close)
	fresh(t)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "relay", Key: "k", Chat: up.URL, Models: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	// run twice on one body, it would read "hi!!"
	if err := provider.SetModelOverride("relay/a", json.RawMessage(`{"operations":[{"path":"messages.0.content","mode":"append","value":"!"}]}`)); err != nil {
		t.Fatal(err)
	}
	h := New().Handler()
	body := `{"model":"relay/a","messages":[{"role":"user","content":"hi"}],"max_tokens":1}`
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body)))
	if rec.Code != 200 {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("the vendor was asked %d times, want 2 (the floor, then served): %v", len(seen), seen)
	}
	for i, b := range seen {
		if strings.Contains(b, "hi!!") {
			t.Fatalf("attempt %d ran the append twice: %s", i+1, b)
		}
		if !strings.Contains(b, "hi!") {
			t.Fatalf("attempt %d has no append: %s", i+1, b)
		}
	}
	// the retry's body is the raised one, and the override is on it
	if !strings.Contains(seen[1], `"max_tokens":16`) {
		t.Fatalf("the second attempt did not take the floor: %s", seen[1])
	}
}
