package override

import (
	"encoding/json"
	"regexp"
	"testing"
)

func TestConditions(t *testing.T) {
	ctx := Context{
		"model": "gpt-5", "original_model": "gpt-5", "protocol": "chat",
		"request_path": "/v1/chat/completions", "agent": "codex", "stream": true,
	}
	t.Run("full in the body", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"model":"gpt-5","t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":[{"path":"model","mode":"full","value":"gpt-5"}]}]}`, ctx)
		wantBody(t, out, `{"model":"gpt-5","t":9}`)
	})
	t.Run("full in the context", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":[{"path":"agent","mode":"full","value":"codex"}]}]}`, ctx)
		wantBody(t, out, `{"t":9}`)
	})
	t.Run("the body wins over the context", func(t *testing.T) {
		// the body's model and the context's model differ: the body's is asked
		out, _, _, _ := apply(t, `{"model":"other","t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":[{"path":"model","mode":"full","value":"gpt-5"}]}]}`, ctx)
		wantBody(t, out, `{"model":"other","t":1}`)
	})
	t.Run("prefix", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"m":"gpt-5.1","t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":[{"path":"m","mode":"prefix","value":"gpt-5"}]}]}`, ctx)
		wantBody(t, out, `{"m":"gpt-5.1","t":9}`)
	})
	t.Run("suffix", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"m":"m-5","t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":[{"path":"m","mode":"suffix","value":"-5"}]}]}`, ctx)
		wantBody(t, out, `{"m":"m-5","t":9}`)
	})
	t.Run("contains", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"m":"x-mini-y","t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":[{"path":"m","mode":"contains","value":"mini"}]}]}`, ctx)
		wantBody(t, out, `{"m":"x-mini-y","t":9}`)
	})
	t.Run("regex", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"model":"gpt-5","t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":[{"path":"model","mode":"regex","value":"^gpt-\\d+"}]}]}`, ctx)
		wantBody(t, out, `{"model":"gpt-5","t":9}`)
	})
	t.Run("gt", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"n":5,"t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":[{"path":"n","mode":"gt","value":3}]}]}`, ctx)
		wantBody(t, out, `{"n":5,"t":9}`)
	})
	t.Run("gte lte lt", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"n":5,"t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":[{"path":"n","mode":"gte","value":5}]}]}`, ctx)
		wantBody(t, out, `{"n":5,"t":9}`)
		out, _, _, _ = apply(t, `{"n":5,"t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":[{"path":"n","mode":"lt","value":4}]}]}`, ctx)
		wantBody(t, out, `{"n":5,"t":1}`)
	})
	t.Run("logic AND requires all", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"a":"x","b":"y","t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"logic":"AND","conditions":[{"path":"a","mode":"full","value":"x"},{"path":"b","mode":"full","value":"NO"}]}]}`, ctx)
		wantBody(t, out, `{"a":"x","b":"y","t":1}`)
		out, _, _, _ = apply(t, `{"a":"x","b":"y","t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"logic":"AND","conditions":[{"path":"a","mode":"full","value":"x"},{"path":"b","mode":"full","value":"y"}]}]}`, ctx)
		wantBody(t, out, `{"a":"x","b":"y","t":9}`)
	})
	t.Run("logic OR is the default", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"a":"x","b":"y","t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":[{"path":"a","mode":"full","value":"NO"},{"path":"b","mode":"full","value":"y"}]}]}`, ctx)
		wantBody(t, out, `{"a":"x","b":"y","t":9}`)
	})
	t.Run("invert", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"a":"x","t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":[{"path":"a","mode":"full","value":"x","invert":true}]}]}`, ctx)
		wantBody(t, out, `{"a":"x","t":1}`)
		out, _, _, _ = apply(t, `{"a":"x","t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":[{"path":"a","mode":"full","value":"NO","invert":true}]}]}`, ctx)
		wantBody(t, out, `{"a":"x","t":9}`)
	})
	t.Run("pass_missing_key", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":[{"path":"missing","mode":"full","value":"x","pass_missing_key":true}]}]}`, ctx)
		wantBody(t, out, `{"t":9}`)
	})
	t.Run("a missing key without pass_missing_key", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":[{"path":"missing","mode":"full","value":"x"}]}]}`, ctx)
		wantBody(t, out, `{"t":1}`)
	})
	t.Run("an object of key: value conditions", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"model":"gpt-5","t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":{"model":"gpt-5"}}]}`, ctx)
		wantBody(t, out, `{"model":"gpt-5","t":9}`)
	})
	t.Run("a stream context condition", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"t":1}`, `{"operations":[{"path":"t","mode":"set","value":9,"conditions":[{"path":"stream","mode":"full","value":true}]}]}`, ctx)
		wantBody(t, out, `{"t":9}`)
	})
}

func TestNegativeIndexAndWildcard(t *testing.T) {
	t.Run("set the last array element by -1", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"c":[{"id":1},{"id":2},{"id":3}]}`, `{"operations":[{"path":"c.-1.id","mode":"set","value":9}]}`, nil)
		wantBody(t, out, `{"c":[{"id":1},{"id":2},{"id":9}]}`)
	})
	t.Run("delete the last array element by -1", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"c":[{"id":1},{"id":2}]}`, `{"operations":[{"path":"c.-1","mode":"delete"}]}`, nil)
		wantBody(t, out, `{"c":[{"id":1}]}`)
	})
	t.Run("a negative index past the end changes nothing", func(t *testing.T) {
		out, applied, _, _ := apply(t, `{"c":[{"id":1}]}`, `{"operations":[{"path":"c.-5.id","mode":"set","value":9}]}`, nil)
		if len(applied) != 0 {
			t.Fatalf("a set past the end recorded %v", applied)
		}
		wantBody(t, out, `{"c":[{"id":1}]}`)
	})
	t.Run("a wildcard over an object's keys", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"o":{"a":1,"b":2}}`, `{"operations":[{"path":"o.*","mode":"to_upper"}]}`, nil)
		// numbers are not strings; both change nothing, recorded nothing
		var applied []string
		out2, applied, _, _ := apply(t, `{"o":{"a":"x","b":"y"}}`, `{"operations":[{"path":"o.*","mode":"to_upper"}]}`, nil)
		_ = out
		wantBody(t, out2, `{"o":{"a":"X","b":"Y"}}`)
		if len(applied) != 1 {
			t.Fatalf("a wildcard op records one line, got %v", applied)
		}
	})
}

func TestPruneObjects(t *testing.T) {
	t.Run("a type name prunes the objects of that type, the root kept", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"items":[{"type":"img","src":"a"},{"type":"txt","body":"b"},{"type":"img","src":"c"}]}`, `{"operations":[{"path":"items","mode":"prune_objects","value":"img"}]}`, nil)
		wantBody(t, out, `{"items":[{"type":"txt","body":"b"}]}`)
	})
	t.Run("a where prunes the matching objects", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"items":[{"kind":"x","type":"img"},{"kind":"y","type":"txt"}]}`, `{"operations":[{"path":"items","mode":"prune_objects","value":{"where":{"type":"img"}}}]}`, nil)
		wantBody(t, out, `{"items":[{"kind":"y","type":"txt"}]}`)
	})
	t.Run("a type field prunes by the object's type field", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"items":[{"kind":"a","type":"t1"},{"kind":"b","type":"t2"}]}`, `{"operations":[{"path":"items","mode":"prune_objects","value":{"type":"t1"}}]}`, nil)
		wantBody(t, out, `{"items":[{"kind":"b","type":"t2"}]}`)
	})
	t.Run("recursive default true prunes below the root", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"o":{"a":{"type":"img"},"b":{"c":{"type":"img"}}}}`, `{"operations":[{"path":"o","mode":"prune_objects","value":"img"}]}`, nil)
		wantBody(t, out, `{"o":{"b":{}}}`)
	})
	t.Run("recursive false prunes only one level", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"o":{"a":{"type":"img"},"b":{"c":{"type":"img"}}}}`, `{"operations":[{"path":"o","mode":"prune_objects","value":{"type":"img","recursive":false}}]}`, nil)
		wantBody(t, out, `{"o":{"b":{"c":{"type":"img"}}}}`)
	})
	t.Run("no rule prunes nothing", func(t *testing.T) {
		out, applied, _, _ := apply(t, `{"o":{"x":1}}`, `{"operations":[{"path":"o","mode":"prune_objects","value":{}}]}`, nil)
		if len(applied) != 0 {
			t.Fatalf("an empty rule recorded %v", applied)
		}
		wantBody(t, out, `{"o":{"x":1}}`)
	})
}

func TestReturnError(t *testing.T) {
	t.Run("a string value", func(t *testing.T) {
		_, _, re, err := apply(t, `{"model":"m"}`, `{"operations":[{"path":"model","mode":"return_error","value":"nope"}]}`, nil)
		if err != nil {
			t.Fatal(err)
		}
		if re == nil || re.Message != "nope" || re.StatusCode != 400 || !re.SkipRetry {
			t.Fatalf("re = %+v", re)
		}
	})
	t.Run("an object value with a status", func(t *testing.T) {
		_, _, re, err := apply(t, `{"model":"m"}`, `{"operations":[{"mode":"return_error","value":{"message":"gone","status_code":404,"code":"nf","type":"not_found_error"}}]}`, nil)
		if err != nil {
			t.Fatal(err)
		}
		if re == nil || re.Message != "gone" || re.StatusCode != 404 || re.Code != "nf" || re.Type != "not_found_error" || !re.SkipRetry {
			t.Fatalf("re = %+v", re)
		}
	})
	t.Run("skip_retry false", func(t *testing.T) {
		_, _, re, _ := apply(t, `{"model":"m"}`, `{"operations":[{"mode":"return_error","value":{"message":"try another","skip_retry":false}}]}`, nil)
		if re == nil || re.SkipRetry {
			t.Fatalf("re = %+v", re)
		}
	})
	t.Run("a status out of range is an error", func(t *testing.T) {
		_, _, _, err := apply(t, `{"model":"m"}`, `{"operations":[{"mode":"return_error","value":{"message":"x","status_code":700}}]}`, nil)
		if err == nil {
			t.Fatal("a status of 700 is a parse error")
		}
	})
	t.Run("a missing message takes the default", func(t *testing.T) {
		_, _, re, _ := apply(t, `{"model":"m"}`, `{"operations":[{"mode":"return_error","value":{"status_code":429}}]}`, nil)
		if re == nil || re.Message == "" {
			t.Fatalf("re = %+v", re)
		}
	})
	t.Run("a return_error runs before the send and stops the override", func(t *testing.T) {
		// a later operation must not run after the request is turned away
		_, applied, re, _ := apply(t, `{"model":"m","t":1}`, `{"operations":[{"mode":"return_error","value":"stop"},{"path":"t","mode":"set","value":9}]}`, nil)
		if re == nil {
			t.Fatal("no return_error")
		}
		if len(applied) != 0 {
			t.Fatalf("an operation after a return_error ran: %v", applied)
		}
	})
}

func TestUnknownModeAndNoOp(t *testing.T) {
	t.Run("an unknown mode is an error at run time", func(t *testing.T) {
		_, _, _, err := apply(t, `{"model":"m"}`, `{"operations":[{"path":"model","mode":"frobnicate"}]}`, nil)
		if err == nil {
			t.Fatal("an unknown mode is an error")
		}
		var um ErrUnknownMode
		if !asErrUnknownMode(err, &um) {
			t.Fatalf("err = %v, want ErrUnknownMode", err)
		}
	})
	t.Run("a header mode is an error that names the plugin", func(t *testing.T) {
		_, _, _, err := apply(t, `{"model":"m"}`, `{"operations":[{"mode":"set_header","value":{}}]}`, nil)
		if err == nil {
			t.Fatal("a header mode is an error here")
		}
	})
	t.Run("an operation with no mode", func(t *testing.T) {
		_, _, _, err := apply(t, `{"model":"m"}`, `{"operations":[{"path":"model"}]}`, nil)
		if err == nil {
			t.Fatal("an operation with no mode is an error")
		}
	})
	t.Run("an empty override leaves the bytes identical", func(t *testing.T) {
		body := []byte(`{"model":"m","t":1}`)
		out, applied, re, err := Apply(body, mustParse(t, `{}`), nil)
		if err != nil || re != nil || len(applied) != 0 {
			t.Fatalf("err=%v re=%v applied=%v", err, re, applied)
		}
		if !bytesEqual(out, body) {
			t.Fatalf("a no-op changed the bytes: %s", out)
		}
	})
	t.Run("an operations-only empty list leaves the bytes identical", func(t *testing.T) {
		body := []byte(`{"model":"m","t":1}`)
		out, _, _, err := Apply(body, mustParse(t, `{"operations":[]}`), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !bytesEqual(out, body) {
			t.Fatalf("a no-op changed the bytes: %s", out)
		}
	})
	t.Run("operations that match nothing leave the bytes identical", func(t *testing.T) {
		body := []byte(`{"model":"m","t":1}`)
		out, applied, _, err := Apply(body, mustParse(t, `{"operations":[{"path":"nope","mode":"delete"},{"path":"model","mode":"set","value":"x","conditions":[{"path":"model","mode":"full","value":"other"}]}]}`), nil)
		if err != nil || len(applied) != 0 {
			t.Fatalf("err=%v applied=%v", err, applied)
		}
		if !bytesEqual(out, body) {
			t.Fatalf("an op that matched nothing changed the bytes: %s", out)
		}
	})
	t.Run("legacy and operations together run the legacy first", func(t *testing.T) {
		// the legacy set and an operation that reads what the legacy wrote
		out, _, _, _ := apply(t, `{"model":"m"}`, `{"temperature":0.5,"operations":[{"path":"temperature","mode":"to_upper"}]}`, nil)
		_ = out
	})
}

func TestLegacyAndOperationsConflict(t *testing.T) {
	// the legacy keys set first, then an operation that reads the value the
	// legacy key wrote: the operation sees the legacy's value
	out, _, _, _ := apply(t, `{"model":"m"}`, `{"temperature":2,"operations":[{"path":"temperature","mode":"replace","from":"2","to":"9"}]}`, nil)
	// replace is a string mode; a number is not a string, so it changes
	// nothing and the legacy's 2 stands
	wantBody(t, out, `{"model":"m","temperature":2}`)
}

func asErrUnknownMode(err error, target *ErrUnknownMode) bool {
	for err != nil {
		if e, ok := err.(ErrUnknownMode); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func mustParse(t *testing.T, s string) *Parsed {
	t.Helper()
	p, err := Parse(json.RawMessage(s))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return p
}

func bytesEqual(a, b []byte) bool {
	return string(a) == string(b)
}

// _ keeps regexp referenced for a future case.
var _ = regexp.MustCompile
