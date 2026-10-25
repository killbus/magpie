package override

import (
	"encoding/json"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// Context is the request identity a condition may match against, after the
// body. The gateway fills it with the request's own: model, original_model,
// request_path, protocol, agent and stream (as the agent-level plugin's
// request object carries them). The engine is agnostic about the transport.
type Context map[string]any

// Context fields the gateway names in a Context.
const (
	CtxModel         = "model"
	CtxOriginalModel = "original_model"
	CtxRequestPath   = "request_path"
	CtxProtocol      = "protocol"
	CtxAgent         = "agent"
	CtxStream        = "stream"
)

// ReturnError is a return_error operation's result: the error the caller
// hands the agent without contacting the upstream. StatusCode and SkipRetry
// keep New API's defaults — 400 and terminate the route — where the value did
// not say otherwise.
type ReturnError struct {
	Message    string
	Code       string
	Type       string
	StatusCode int
	SkipRetry  bool
}

// Apply runs p's legacy keys and operations over body, in order, and gives the
// new body, one audit line per operation that actually changed it, a
// ReturnError when a return_error operation turned the request away, and an
// error when an operation this engine does not run is asked. An empty
// override is a no-op: body is returned as it is, not decoded.
func Apply(body []byte, p *Parsed, ctx Context) (out []byte, applied []string, re *ReturnError, err error) {
	if p == nil || p.Empty {
		return body, nil, nil, nil
	}
	out = body
	for _, kv := range p.Legacy {
		n, e := sjson.SetRawBytes(out, escapePathPiece(kv.Key), kv.Value)
		if e != nil {
			return nil, nil, nil, e
		}
		out = n
		applied = append(applied, auditSet(kv.Key, kv.Value))
	}
	for i := range p.Ops {
		op := &p.Ops[i]
		if !when(out, ctx, op) {
			continue
		}
		n, r, e := applyOp(out, op, ctx)
		if e != nil {
			return nil, nil, nil, e
		}
		if r != nil {
			return out, applied, r, nil
		}
		if n != nil {
			out = n
			applied = append(applied, auditLine(op, out))
		}
	}
	return out, applied, nil, nil
}

// escapePathPiece is a legacy top-level key as the path sjson sets it as one
// literal key: a dot or an asterisk in the key is escaped, so "a.b" is the
// key "a.b" and "*" the key "*", not a path through "a" or a wildcard.
func escapePathPiece(key string) string {
	return strings.NewReplacer(".", `\.`).Replace(strings.NewReplacer("*", `\*`).Replace(key))
}

// when says whether op's conditions hold: all of them when Logic is AND, any
// of them otherwise (New API's default). No conditions hold. A condition's
// path is looked up in the body first and then in ctx; a path in neither is
// the condition's PassMissingKey.
func when(body []byte, ctx Context, op *Operation) bool {
	if len(op.Conditions) == 0 {
		return true
	}
	and := strings.EqualFold(strings.TrimSpace(op.Logic), "AND")
	for i := range op.Conditions {
		hit := conditionHit(body, ctx, &op.Conditions[i])
		if and && !hit {
			return false
		}
		if !and && hit {
			return true
		}
	}
	return and
}

// conditionHit is one condition: its value in the body, else ctx, compared by
// its mode to its value, inverted where the condition inverts.
func conditionHit(body []byte, ctx Context, c *Condition) bool {
	v, found := lookup(body, c.Path)
	if !found {
		if cv, ok := ctx[c.Path]; ok {
			v, found = cv, true
		}
	}
	if !found {
		return c.PassMissingKey
	}
	mode := strings.ToLower(strings.TrimSpace(c.Mode))
	if mode == "" {
		mode = "full"
	}
	r := compare(v, c.Value, mode)
	if c.Invert {
		return !r
	}
	return r
}

// lookup is a path's value in the body, found being whether the body has it at
// all. A path that resolves more than one way is not a value a condition can
// compare, so it is not found.
func lookup(body []byte, path string) (any, bool) {
	if path == "" {
		return nil, false
	}
	ps := resolve(body, path)
	if len(ps) != 1 {
		return nil, false
	}
	r := gjson.GetBytes(body, ps[0])
	if !r.Exists() {
		return nil, false
	}
	return jsonValueOf(r), true
}

// jsonValueOf is a gjson result as the value a condition compares: a string,
// a number, a bool, null, or a raw JSON object or array kept whole.
func jsonValueOf(r gjson.Result) any {
	switch r.Type {
	case gjson.String:
		return r.Str
	case gjson.Number:
		return r.Num
	case gjson.True:
		return true
	case gjson.False:
		return false
	case gjson.Null:
		return nil
	default:
		return r.Raw
	}
}

// compare is one condition's mode run over a found value and its target.
func compare(v any, target json.RawMessage, mode string) bool {
	t := targetValue(target)
	switch mode {
	case "full":
		return fullEq(v, t)
	case "prefix":
		return strings.HasPrefix(stringOf(v), stringOf(t))
	case "suffix":
		return strings.HasSuffix(stringOf(v), stringOf(t))
	case "contains":
		return strings.Contains(stringOf(v), stringOf(t))
	case "regex":
		re, err := regexp.Compile(stringOf(t))
		if err != nil {
			return false
		}
		return re.MatchString(stringOf(v))
	case "gt", "gte", "lt", "lte":
		a, ok1 := asNumber(v)
		b, ok2 := asNumber(t)
		if !ok1 || !ok2 {
			return false
		}
		switch mode {
		case "gt":
			return a > b
		case "gte":
			return a >= b
		case "lt":
			return a < b
		default:
			return a <= b
		}
	}
	return false
}

// stringOf is a value as a condition's prefix/suffix/contains/regex compares
// it: a string as-is, everything else its JSON.
func stringOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}

func asNumber(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	}
	return 0, false
}

// fullEq is the full mode: a string to a string, a number to a number, a
// bool, null, or an object or array as its raw JSON (key order left to the
// value's own spelling, as New API compares its objects).
func fullEq(v, t any) bool {
	if vs, ok := v.(string); ok {
		ts, ok := t.(string)
		return ok && vs == ts
	}
	if vn, ok := asNumber(v); ok {
		tn, ok := asNumber(t)
		return ok && vn == tn
	}
	if vb, ok := v.(bool); ok {
		tb, ok := t.(bool)
		return ok && vb == tb
	}
	if v == nil {
		return t == nil
	}
	return stringOf(v) == stringOf(t)
}

// targetValue is a condition's value JSON as the value compared: a string,
// number, bool, null, or the raw object or array kept whole.
func targetValue(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	r := gjson.ParseBytes(raw)
	switch r.Type {
	case gjson.String:
		return r.Str
	case gjson.Number:
		return r.Num
	case gjson.True:
		return true
	case gjson.False:
		return false
	case gjson.Null:
		return nil
	default:
		return r.Raw
	}
}

// isJSONObject is whether a gjson result is a JSON object: gjson keeps an
// object and an array as the same JSON type, told apart by the value's first
// byte.
func isJSONObject(r gjson.Result) bool {
	return r.Type == gjson.JSON && len(r.Raw) > 0 && r.Raw[0] == '{'
}

// isJSONArray is whether a gjson result is a JSON array.
func isJSONArray(r gjson.Result) bool {
	return r.Type == gjson.JSON && len(r.Raw) > 0 && r.Raw[0] == '['
}

// resolve is the concrete paths a gjson path stands for in body: * is each
// key or index (a key in sorted order, as New API reads them), -1 an array's
// last. A path that matches nothing resolves to nothing; the empty path
// resolves to the body alone.
func resolve(body []byte, path string) []string {
	keys := strings.Split(path, ".")
	var paths []string
	paths = append(paths, "")
	for _, k := range keys {
		if k == "" {
			continue
		}
		next := paths[:0:0]
		for _, p := range paths {
			node := gjson.GetBytes(body, p)
			if k == "*" {
				switch {
				case isJSONObject(node):
					m := node.Map()
					ks := make([]string, 0, len(m))
					for key := range m {
						ks = append(ks, key)
					}
					sort.Strings(ks)
					for _, key := range ks {
						next = append(next, joinPath(p, key))
					}
				case isJSONArray(node):
					for i := range node.Array() {
						next = append(next, joinPath(p, strconv.Itoa(i)))
					}
				}
				continue
			}
			if isJSONArray(node) {
				if i, ok := asIndex(k, len(node.Array())); ok {
					next = append(next, joinPath(p, strconv.Itoa(i)))
				}
				continue
			}
			// an object, or a path that makes one where it is not there yet
			// (set creates it): the key resolves.
			next = append(next, joinPath(p, k))
		}
		paths = next
	}
	return paths
}

// joinPath is a resolved path with one more key, as gjson spells it: the key
// is joined with its separators left, and a bare key is itself.
func joinPath(p, k string) string {
	if p == "" {
		return k
	}
	return p + "." + k
}

// asIndex is a path piece as an array index, a negative one counted from the
// end: -1 the last. A piece that is not an index, or an index past the end,
// is not an index.
func asIndex(k string, length int) (int, bool) {
	n, err := strconv.Atoi(k)
	if err != nil {
		return 0, false
	}
	if n < 0 {
		n += length
	}
	if n < 0 || n >= length {
		return 0, false
	}
	return n, true
}
