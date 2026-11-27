package gateway

// A param override is the New API param_override JSON the user gave a model
// (provider.SetModelOverride): the last change made to the body, after
// magpie's own rewrites and the user's extra fields, before it is signed and
// sent. This is where it is applied, once per outbound attempt.
//
// The body's own "model" field is the vendor's name for a translated
// request, not the model the user configured, so the override is selected
// from the request's identity carried on the context (overrideAt), not read
// out of the body.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/provider/override"
	"github.com/yetone/magpie/internal/settings"
)

// overrideAt is what a try knows of the override it is to run: the model it is
// for, the request's identity for a condition to match, and what it did — the
// audit lines of the operations that changed the body, and the return_error
// that turned the request away.
type overrideAt struct {
	model  string // the model magpie knows it by, not the wire name
	agent  string
	stream bool

	applied  []string
	turnAway *override.ReturnError
}

type overrideAtKey struct{}

// withOverrideAt is ctx carrying o, which a try's outbound requests read.
func withOverrideAt(ctx context.Context, o *overrideAt) context.Context {
	return context.WithValue(ctx, overrideAtKey{}, o)
}

// overrideOf is the override a request under ctx is run under, nil where it
// has none.
func overrideOf(ctx context.Context) *overrideAt {
	o, _ := ctx.Value(overrideAtKey{}).(*overrideAt)
	return o
}

// overrideBody is the override applied to body once: the body to send, the
// audit lines of what changed it, and the error a return_error turns the
// request away with. A no-op override gives body back as it is, not decoded.
func overrideBody(ctx context.Context, p provider.Provider, to provider.Protocol, path string, body []byte) ([]byte, *override.ReturnError, error) {
	o := overrideOf(ctx)
	if o == nil {
		return body, nil, nil
	}
	raw, ok := provider.ModelOverrideOf(settings.Shared(), p.ID, o.model)
	if !ok {
		return body, nil, nil
	}
	// the save-time rules first (Validate: the root model protected, every
	// mode one this engine runs), so an override a hand-edited settings.json
	// left unrunnable is not run: the request goes as it would without it,
	// rather than failing at the wire on every call
	if _, err := override.Validate(raw); err != nil {
		return body, nil, nil
	}
	parsed, err := override.Parse(raw)
	if err != nil {
		return body, nil, nil
	}
	out, applied, re, err := override.Apply(body, parsed, override.Context{
		override.CtxModel:         o.model,
		override.CtxOriginalModel: o.model,
		override.CtxRequestPath:   path,
		override.CtxProtocol:      string(to),
		override.CtxAgent:         o.agent,
		override.CtxStream:        o.stream,
	})
	if err != nil {
		// an override that can't be run leaves the body as magpie built it,
		// as an unreadable one does: a save refuses these (SetModelOverride
		// validates), so this is a hand-edited settings.json, and failing
		// every request over it is worse than not running it
		return body, nil, nil
	}
	o.applied = applied
	if re != nil {
		o.turnAway = re
		return body, re, nil
	}
	return out, nil, nil
}

// overrideReply is the answer a return_error gives the agent, as if its
// vendor had said it: the configured status and an OpenAI-shaped error, so
// the routing loop sees a normal reply of that status.
func overrideReply(req *http.Request, re *override.ReturnError) *http.Response {
	code, typ := re.Code, re.Type
	if code == "" {
		code = "invalid_request"
	}
	if typ == "" {
		typ = "invalid_request_error"
	}
	b, _ := json.Marshal(map[string]any{"error": map[string]any{
		"message": re.Message, "type": typ, "code": code,
	}})
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	h.Set("Content-Length", strconv.Itoa(len(b)))
	status := re.StatusCode
	if status < 100 || status > 599 {
		status = http.StatusBadRequest
	}
	return &http.Response{
		Status: strconv.Itoa(status) + " " + http.StatusText(status), StatusCode: status,
		Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: h, Body: io.NopCloser(bytes.NewReader(b)), ContentLength: int64(len(b)), Request: req,
	}
}
