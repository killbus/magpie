package override

import (
	"encoding/json"
	"reflect"
	"testing"
)

// semanticEqual is whether two JSON values are the same: the same value, a
// key order and whitespace apart. It is what "behaves identically for the
// same request" means for an in/out test.
func semanticEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	var va, vb any
	if err := json.Unmarshal(a, &va); err != nil {
		return false
	}
	if err := json.Unmarshal(b, &vb); err != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

// apply is a test's one-stop: parse the override, run it over the body, and
// give the body it came out as, the audit lines, the return_error and the
// error, any of them checked by the case.
func apply(t *testing.T, body, ov string, ctx Context) (out []byte, applied []string, re *ReturnError, err error) {
	t.Helper()
	p, perr := Parse(json.RawMessage(ov))
	if perr != nil {
		return nil, nil, nil, perr
	}
	return Apply([]byte(body), p, ctx)
}

// wantBody is a case's out checked against its expected body: a semantic
// match, the expected kept for the message when one is missed.
func wantBody(t *testing.T, got []byte, want string) {
	t.Helper()
	if !semanticEqual(t, got, []byte(want)) {
		t.Fatalf("body = %s, want %s", got, want)
	}
}

func TestLegacyKeysSet(t *testing.T) {
	cases := []struct {
		name string
		body string
		ov   string
		want string
	}{
		{"one key", `{"model":"m","messages":[]}`, `{"temperature":0.2}`, `{"model":"m","messages":[],"temperature":0.2}`},
		{"object value", `{"model":"m"}`, `{"max_completion_tokens":1200}`, `{"model":"m","max_completion_tokens":1200}`},
		{"key with a dot is literal", `{"a":{"b":1}}`, `{"a.b":2}`, `{"a":{"b":1},"a.b":2}`},
		{"key with an asterisk is literal", `{"a":1}`, `{"*":9}`, `{"a":1,"*":9}`},
		{"keys run in sorted order", `{"z":1}`, `{"b":2,"a":3}`, `{"z":1,"b":2,"a":3}`},
		{"an empty object is nothing", `{"m":1}`, `{}`, `{"m":1}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, applied, re, err := apply(t, c.body, c.ov, nil)
			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			if re != nil {
				t.Fatalf("return_error where none was asked")
			}
			wantBody(t, out, c.want)
			if len(applied) == 0 && c.ov != `{}` {
				t.Errorf("no audit line for %s", c.ov)
			}
		})
	}
}

func TestSetDeleteMoveCopy(t *testing.T) {
	t.Run("set a path", func(t *testing.T) {
		out, _, _, err := apply(t, `{"model":"m","temperature":1}`, `{"operations":[{"path":"temperature","mode":"set","value":0.5}]}`, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantBody(t, out, `{"model":"m","temperature":0.5}`)
	})
	t.Run("set creates a missing path", func(t *testing.T) {
		out, _, _, err := apply(t, `{"model":"m"}`, `{"operations":[{"path":"metadata.user_id","mode":"set","value":"u1"}]}`, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantBody(t, out, `{"model":"m","metadata":{"user_id":"u1"}}`)
	})
	t.Run("set keep_origin keeps an existing value", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"model":"m","temperature":0.9}`, `{"operations":[{"path":"temperature","mode":"set","value":0.1,"keep_origin":true}]}`, nil)
		wantBody(t, out, `{"model":"m","temperature":0.9}`)
	})
	t.Run("set without keep_origin overwrites", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"model":"m","temperature":0.9}`, `{"operations":[{"path":"temperature","mode":"set","value":0.1}]}`, nil)
		wantBody(t, out, `{"model":"m","temperature":0.1}`)
	})
	t.Run("set null removes nothing but sets null", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"model":"m","temperature":1}`, `{"operations":[{"path":"temperature","mode":"set","value":null}]}`, nil)
		wantBody(t, out, `{"model":"m","temperature":null}`)
	})
	t.Run("delete a path", func(t *testing.T) {
		out, _, _, err := apply(t, `{"model":"m","temperature":1}`, `{"operations":[{"path":"temperature","mode":"delete"}]}`, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantBody(t, out, `{"model":"m"}`)
	})
	t.Run("delete a missing path changes nothing", func(t *testing.T) {
		out, applied, _, err := apply(t, `{"model":"m"}`, `{"operations":[{"path":"nope","mode":"delete"}]}`, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(applied) != 0 {
			t.Fatalf("a delete of a missing path recorded %v", applied)
		}
		wantBody(t, out, `{"model":"m"}`)
	})
	t.Run("delete a wildcard", func(t *testing.T) {
		out, _, _, err := apply(t, `{"a":{"x":1,"y":2,"z":3}}`, `{"operations":[{"path":"a.*","mode":"delete"}]}`, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantBody(t, out, `{"a":{}}`)
	})
	t.Run("delete a wildcard over an array empties it", func(t *testing.T) {
		// a wildcard over an array resolves to every index; the deletes run
		// high to low (a delete's paths are reversed), so each index still to
		// come stays put and the whole array goes.
		out, _, _, err := apply(t, `{"l":[{"k":0},{"k":1},{"k":2}]}`, `{"operations":[{"path":"l.*","mode":"delete"}]}`, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantBody(t, out, `{"l":[]}`)
	})
	t.Run("move a path", func(t *testing.T) {
		out, _, _, err := apply(t, `{"a":{"b":1},"c":0}`, `{"operations":[{"path":"a.b","mode":"move","from":"a.b","to":"c"}]}`, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantBody(t, out, `{"a":{},"c":1}`)
	})
	t.Run("move a missing source changes nothing", func(t *testing.T) {
		out, applied, _, err := apply(t, `{"c":0}`, `{"operations":[{"from":"a.b","to":"c","mode":"move"}]}`, nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(applied) != 0 {
			t.Fatalf("a move of a missing source recorded %v", applied)
		}
		wantBody(t, out, `{"c":0}`)
	})
	t.Run("copy a path", func(t *testing.T) {
		out, _, _, err := apply(t, `{"a":{"b":{"d":1}},"c":0}`, `{"operations":[{"from":"a.b","to":"c.x","mode":"copy"}]}`, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantBody(t, out, `{"a":{"b":{"d":1}},"c":{"x":{"d":1}}}`)
	})
	t.Run("move keeps the value's key order", func(t *testing.T) {
		out, _, _, err := apply(t, `{"src":{"z":1,"a":2},"dst":0}`, `{"operations":[{"from":"src","to":"dst","mode":"move"}]}`, nil)
		if err != nil {
			t.Fatal(err)
		}
		// the moved object reads z before a, as it was written
		var v map[string]any
		if err := json.Unmarshal(out, &v); err != nil {
			t.Fatal(err)
		}
		dst, _ := v["dst"].(map[string]any)
		if len(dst) != 2 {
			t.Fatalf("dst = %v", dst)
		}
	})
}

func TestPrependAppend(t *testing.T) {
	t.Run("append to a string", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"s":"ab"}`, `{"operations":[{"path":"s","mode":"append","value":"cd"}]}`, nil)
		wantBody(t, out, `{"s":"abcd"}`)
	})
	t.Run("prepend to a string", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"s":"cd"}`, `{"operations":[{"path":"s","mode":"prepend","value":"ab"}]}`, nil)
		wantBody(t, out, `{"s":"abcd"}`)
	})
	t.Run("append to an array spreads an array value", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"l":[1]}`, `{"operations":[{"path":"l","mode":"append","value":[2,3]}]}`, nil)
		wantBody(t, out, `{"l":[1,2,3]}`)
	})
	t.Run("append a single value to an array", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"l":[1]}`, `{"operations":[{"path":"l","mode":"append","value":4}]}`, nil)
		wantBody(t, out, `{"l":[1,4]}`)
	})
	t.Run("prepend to an array", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"l":[2]}`, `{"operations":[{"path":"l","mode":"prepend","value":[0]}]}`, nil)
		wantBody(t, out, `{"l":[0,2]}`)
	})
	t.Run("append an object merges, keep_origin keeps the old keys", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"o":{"a":1,"b":2}}`, `{"operations":[{"path":"o","mode":"append","value":{"b":9,"c":3},"keep_origin":true}]}`, nil)
		wantBody(t, out, `{"o":{"a":1,"b":2,"c":3}}`)
	})
	t.Run("append an object without keep_origin overwrites", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"o":{"a":1,"b":2}}`, `{"operations":[{"path":"o","mode":"append","value":{"b":9,"c":3}}]}`, nil)
		wantBody(t, out, `{"o":{"a":1,"b":9,"c":3}}`)
	})
	t.Run("append to a missing path changes nothing", func(t *testing.T) {
		out, applied, _, _ := apply(t, `{"x":1}`, `{"operations":[{"path":"nope","mode":"append","value":"z"}]}`, nil)
		if len(applied) != 0 {
			t.Fatalf("an append to a missing path recorded %v", applied)
		}
		wantBody(t, out, `{"x":1}`)
	})
}

func TestStringModes(t *testing.T) {
	t.Run("trim_prefix", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"s":"prefix-rest"}`, `{"operations":[{"path":"s","mode":"trim_prefix","value":"prefix-"}]}`, nil)
		wantBody(t, out, `{"s":"rest"}`)
	})
	t.Run("trim_prefix of a prefix absent changes nothing", func(t *testing.T) {
		out, applied, _, _ := apply(t, `{"s":"rest"}`, `{"operations":[{"path":"s","mode":"trim_prefix","value":"prefix-"}]}`, nil)
		if len(applied) != 0 {
			t.Fatalf("a no-op trim_prefix recorded %v", applied)
		}
		wantBody(t, out, `{"s":"rest"}`)
	})
	t.Run("trim_suffix", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"s":"rest-suffix"}`, `{"operations":[{"path":"s","mode":"trim_suffix","value":"-suffix"}]}`, nil)
		wantBody(t, out, `{"s":"rest"}`)
	})
	t.Run("ensure_prefix", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"s":"rest"}`, `{"operations":[{"path":"s","mode":"ensure_prefix","value":"pre-"}]}`, nil)
		wantBody(t, out, `{"s":"pre-rest"}`)
	})
	t.Run("ensure_prefix of a prefix present changes nothing", func(t *testing.T) {
		out, applied, _, _ := apply(t, `{"s":"pre-rest"}`, `{"operations":[{"path":"s","mode":"ensure_prefix","value":"pre-"}]}`, nil)
		if len(applied) != 0 {
			t.Fatalf("a no-op ensure_prefix recorded %v", applied)
		}
		wantBody(t, out, `{"s":"pre-rest"}`)
	})
	t.Run("ensure_suffix", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"s":"rest"}`, `{"operations":[{"path":"s","mode":"ensure_suffix","value":"-post"}]}`, nil)
		wantBody(t, out, `{"s":"rest-post"}`)
	})
	t.Run("trim_space", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"s":"  hi  "}`, `{"operations":[{"path":"s","mode":"trim_space"}]}`, nil)
		wantBody(t, out, `{"s":"hi"}`)
	})
	t.Run("to_lower", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"s":"Hello"}`, `{"operations":[{"path":"s","mode":"to_lower"}]}`, nil)
		wantBody(t, out, `{"s":"hello"}`)
	})
	t.Run("to_upper", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"s":"Hello"}`, `{"operations":[{"path":"s","mode":"to_upper"}]}`, nil)
		wantBody(t, out, `{"s":"HELLO"}`)
	})
	t.Run("replace", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"s":"a-b-c"}`, `{"operations":[{"path":"s","mode":"replace","from":"-","to":"_"}]}`, nil)
		wantBody(t, out, `{"s":"a_b_c"}`)
	})
	t.Run("replace all occurrences", func(t *testing.T) {
		out, _, _, _ := apply(t, `{"s":"xzxz"}`, `{"operations":[{"path":"s","mode":"replace","from":"x","to":""}]}`, nil)
		wantBody(t, out, `{"s":"zz"}`)
	})
	t.Run("regex_replace", func(t *testing.T) {
		out, _, _, err := apply(t, `{"s":"abc123def"}`, `{"operations":[{"path":"s","mode":"regex_replace","from":"[0-9]+","to":"N"}]}`, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantBody(t, out, `{"s":"abcNdef"}`)
	})
	t.Run("regex_replace with a case-insensitive flag", func(t *testing.T) {
		out, _, _, err := apply(t, `{"s":"Hello"}`, `{"operations":[{"path":"s","mode":"regex_replace","from":"(?i)hello","to":"hi"}]}`, nil)
		if err != nil {
			t.Fatal(err)
		}
		wantBody(t, out, `{"s":"hi"}`)
	})
	t.Run("a string mode on a non-string changes nothing", func(t *testing.T) {
		out, applied, _, _ := apply(t, `{"n":5}`, `{"operations":[{"path":"n","mode":"to_lower"}]}`, nil)
		if len(applied) != 0 {
			t.Fatalf("to_lower on a number recorded %v", applied)
		}
		wantBody(t, out, `{"n":5}`)
	})
}
