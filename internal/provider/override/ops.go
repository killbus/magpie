package override

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// applyOp runs one operation that its conditions let through: it gives the
// changed body (nil when nothing changed), the ReturnError a return_error
// operation makes, and an error an operation this engine does not run.
func applyOp(body []byte, op *Operation, ctx Context) (out []byte, re *ReturnError, err error) {
	mode := op.Mode
	if !BodyModes[mode] {
		return nil, nil, ErrUnknownMode{Mode: mode}
	}
	switch mode {
	case "return_error":
		e, perr := parseReturnError(op.Value)
		if perr != nil {
			return nil, nil, perr
		}
		return nil, e, nil
	case "move", "copy":
		return moveOrCopy(body, op, mode == "move")
	case "prune_objects":
		return pruneObjectsOp(body, op, ctx)
	default:
		return singleOrMany(body, op)
	}
}

// singleOrMany runs a path-based mode over every path it resolves to, one
// value each. A delete over an array's indices runs from the end, so the
// indices still to come stay in place.
func singleOrMany(body []byte, op *Operation) (out []byte, re *ReturnError, err error) {
	paths := resolve(body, op.Path)
	if len(paths) == 0 {
		return nil, nil, nil
	}
	if op.Mode == "delete" {
		slices.Reverse(paths)
	}
	changed := false
	n := body
	for _, pth := range paths {
		r, e := runOneMode(n, pth, op)
		if e != nil {
			return nil, nil, e
		}
		if r != nil {
			n = r
			changed = true
		}
	}
	if !changed {
		return nil, nil, nil
	}
	return n, nil, nil
}

// runOneMode is a path-based mode at one concrete path: the new body, or nil
// when it changed nothing there.
func runOneMode(body []byte, pth string, op *Operation) (out []byte, err error) {
	cur := gjson.GetBytes(body, pth)
	switch op.Mode {
	case "set":
		if op.KeepOrigin && cur.Exists() {
			return nil, nil
		}
		return sjson.SetRawBytes(body, pth, rawOrNull(op.Value))
	case "delete":
		if !cur.Exists() {
			return nil, nil
		}
		return sjson.DeleteBytes(body, pth)
	case "prepend", "append":
		return joinOp(body, pth, cur, op, op.Mode == "prepend")
	case "trim_prefix", "trim_suffix", "ensure_prefix", "ensure_suffix":
		if cur.Type != gjson.String || len(op.Value) == 0 {
			return nil, nil
		}
		v := stringOfJSON(op.Value)
		var next string
		switch op.Mode {
		case "trim_prefix":
			if strings.HasPrefix(cur.Str, v) {
				next = cur.Str[len(v):]
			} else {
				next = cur.Str
			}
		case "trim_suffix":
			if v != "" && strings.HasSuffix(cur.Str, v) {
				next = cur.Str[:len(cur.Str)-len(v)]
			} else {
				next = cur.Str
			}
		case "ensure_prefix":
			if strings.HasPrefix(cur.Str, v) {
				next = cur.Str
			} else {
				next = v + cur.Str
			}
		default:
			if strings.HasSuffix(cur.Str, v) {
				next = cur.Str
			} else {
				next = cur.Str + v
			}
		}
		if next == cur.Str {
			return nil, nil
		}
		return sjson.SetBytes(body, pth, next)
	case "trim_space":
		if cur.Type != gjson.String {
			return nil, nil
		}
		next := strings.TrimSpace(cur.Str)
		if next == cur.Str {
			return nil, nil
		}
		return sjson.SetBytes(body, pth, next)
	case "to_lower", "to_upper":
		if cur.Type != gjson.String {
			return nil, nil
		}
		var next string
		if op.Mode == "to_lower" {
			next = strings.ToLower(cur.Str)
		} else {
			next = strings.ToUpper(cur.Str)
		}
		if next == cur.Str {
			return nil, nil
		}
		return sjson.SetBytes(body, pth, next)
	case "replace":
		if cur.Type != gjson.String || op.From == "" {
			return nil, nil
		}
		next := strings.ReplaceAll(cur.Str, op.From, op.To)
		if next == cur.Str {
			return nil, nil
		}
		return sjson.SetBytes(body, pth, next)
	case "regex_replace":
		if cur.Type != gjson.String || op.From == "" {
			return nil, nil
		}
		re, err := regexp.Compile(op.From)
		if err != nil {
			return nil, fmt.Errorf("regex_replace: %w", err)
		}
		next := re.ReplaceAllString(cur.Str, op.To)
		if next == cur.Str {
			return nil, nil
		}
		return sjson.SetBytes(body, pth, next)
	}
	return nil, nil
}

// rawOrNull is an operation's value JSON, null where it gave none.
func rawOrNull(v json.RawMessage) []byte {
	if len(v) == 0 {
		return []byte("null")
	}
	return v
}

// stringOfJSON is a value's JSON as the string a string mode acts with: a
// JSON string as its content, anything else as its JSON.
func stringOfJSON(v json.RawMessage) string {
	if len(v) == 0 {
		return ""
	}
	r := gjson.ParseBytes(v)
	if r.Type == gjson.String {
		return r.Str
	}
	return r.Raw
}

// joinOp is a prepend or append at one path: arrays joined (an array value
// spread in), strings joined, objects merged (KeepOrigin keeping the keys
// already there).
func joinOp(body []byte, pth string, cur gjson.Result, op *Operation, before bool) (out []byte, err error) {
	add := rawOrNull(op.Value)
	switch {
	case isJSONArray(cur):
		var curItems []json.RawMessage
		if err := json.Unmarshal([]byte(cur.Raw), &curItems); err != nil {
			return nil, nil
		}
		var addItems []json.RawMessage
		if isJSONArray(gjson.ParseBytes(add)) {
			if err := json.Unmarshal(add, &addItems); err != nil {
				return nil, nil
			}
		} else {
			addItems = []json.RawMessage{add}
		}
		joined := make([]json.RawMessage, 0, len(curItems)+len(addItems))
		if before {
			joined = append(joined, addItems...)
			joined = append(joined, curItems...)
		} else {
			joined = append(joined, curItems...)
			joined = append(joined, addItems...)
		}
		b, e := json.Marshal(joined)
		if e != nil {
			return nil, e
		}
		return sjson.SetRawBytes(body, pth, b)
	case cur.Type == gjson.String:
		v := stringOfJSON(add)
		var next string
		if before {
			next = v + cur.Str
		} else {
			next = cur.Str + v
		}
		return sjson.SetBytes(body, pth, next)
	case isJSONObject(cur):
		if !isJSONObject(gjson.ParseBytes(add)) {
			return nil, nil
		}
		var curObj, addObj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(cur.Raw), &curObj); err != nil {
			return nil, nil
		}
		if err := json.Unmarshal(add, &addObj); err != nil {
			return nil, nil
		}
		merged := map[string]json.RawMessage{}
		keys := make([]string, 0, len(curObj)+len(addObj))
		for k, v := range curObj {
			merged[k] = v
			keys = append(keys, k)
		}
		for k, v := range addObj {
			if op.KeepOrigin {
				if _, ok := merged[k]; ok {
					continue
				}
			}
			merged[k] = v
			keys = append(keys, k)
		}
		b, e := marshalOrdered(cur.Raw, merged)
		if e != nil {
			return nil, e
		}
		return sjson.SetRawBytes(body, pth, b)
	}
	return nil, nil
}

// marshalOrdered is a merged object as JSON, its keys in the order they came:
// the original's in the original's order first, the new ones after, in sorted
// order, so a merge does not reorder what was already there.
func marshalOrdered(original string, out map[string]json.RawMessage) ([]byte, error) {
	// the original's keys in its own order: gjson's Map is unordered, so the
	// original is walked key by key as it reads
	seen := map[string]bool{}
	var ordered []string
	dec := json.NewDecoder(strings.NewReader(original))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("not a JSON object")
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("not an object key")
		}
		ordered = append(ordered, key)
		seen[key] = true
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, err
		}
	}
	var extra []string
	for k := range out {
		if !seen[k] {
			extra = append(extra, k)
		}
	}
	sort.Strings(extra)
	ordered = append(ordered, extra...)
	var sb strings.Builder
	sb.WriteByte('{')
	for i, k := range ordered {
		if i > 0 {
			sb.WriteByte(',')
		}
		b, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		sb.Write(b)
		sb.WriteByte(':')
		sb.Write(out[k])
	}
	sb.WriteByte('}')
	return []byte(sb.String()), nil
}

// moveOrCopy is a move or copy: the source's value written to the destination
// as its raw bytes (so a value's own key order is kept), the source taken
// away on a move. A source or destination that resolves more than one way, or
// a source that is not there, changes nothing.
func moveOrCopy(body []byte, op *Operation, move bool) (out []byte, re *ReturnError, err error) {
	froms := resolve(body, op.From)
	tos := resolve(body, op.To)
	if len(froms) != 1 || len(tos) != 1 {
		return nil, nil, nil
	}
	src := gjson.GetBytes(body, froms[0])
	if !src.Exists() {
		return nil, nil, nil
	}
	n, e := sjson.SetRawBytes(body, tos[0], []byte(src.Raw))
	if e != nil {
		return nil, nil, e
	}
	if !move {
		return n, nil, nil
	}
	n, e = sjson.DeleteBytes(n, froms[0])
	return n, nil, e
}

// pruneObjectsOp is prune_objects: every object below the path the rule
// matches is taken out, the path's own value kept. A rule is a type name, or
// an object with type, where, conditions, logic (default AND) and recursive
// (default true).
func pruneObjectsOp(body []byte, op *Operation, ctx Context) (out []byte, re *ReturnError, err error) {
	rule, err := parsePruneRule(op.Value)
	if err != nil {
		return nil, nil, err
	}
	if rule.empty() {
		return nil, nil, nil
	}
	var paths []string
	if op.Path == "" {
		paths = []string{""} // the root, kept
	} else {
		paths = resolve(body, op.Path)
		if len(paths) == 0 {
			return nil, nil, nil
		}
	}
	n := body
	changed := false
	for _, pth := range paths {
		cur := gjson.GetBytes(n, pth)
		if !cur.Exists() || (!isJSONObject(cur) && !isJSONArray(cur)) {
			continue
		}
		var v any
		if err := json.Unmarshal([]byte(cur.Raw), &v); err != nil {
			continue
		}
		pv, pruned := prune(v, rule, ctx)
		// a node the rule pruned nothing leaves the bytes as they are, so a
		// no-op prune is a no-op on the body.
		if !pruned {
			continue
		}
		b, e := json.Marshal(pv)
		if e != nil {
			return nil, nil, e
		}
		m, e := sjson.SetRawBytes(n, pth, b)
		if e != nil {
			return nil, nil, e
		}
		n = m
		changed = true
	}
	if !changed {
		return nil, nil, nil
	}
	return n, nil, nil
}

// pruneRule is prune_objects' value read to the conditions it matches.
type pruneRule struct {
	Conds []Condition
	Logic string
	Rec   bool
}

func (r *pruneRule) empty() bool { return len(r.Conds) == 0 }

func parsePruneRule(v json.RawMessage) (*pruneRule, error) {
	r := &pruneRule{Logic: "AND", Rec: true}
	rk := gjson.ParseBytes(v)
	switch {
	case len(v) == 0 || rk.Type == gjson.String:
		s := strings.TrimSpace(rk.Str)
		if s != "" {
			r.Conds = append(r.Conds, Condition{Path: "type", Mode: "full", Value: rawOrNull(mustMarshal(s))})
		}
	case isJSONObject(rk):
		var obj struct {
			Type       any             `json:"type"`
			Where      json.RawMessage `json:"where"`
			Conditions json.RawMessage `json:"conditions"`
			Logic      string          `json:"logic"`
			Recursive  *bool           `json:"recursive"`
		}
		if err := json.Unmarshal(v, &obj); err != nil {
			return nil, err
		}
		if obj.Logic != "" {
			r.Logic = obj.Logic
		}
		if obj.Recursive != nil {
			r.Rec = *obj.Recursive
		}
		// conditions absent is no conditions, as the plugin's conditions() is
		// for it: an object rule of just {type} or {where} is the common form.
		if len(obj.Conditions) > 0 {
			conds, err := parseConditions(obj.Conditions)
			if err != nil {
				return nil, err
			}
			r.Conds = append(r.Conds, conds...)
		}
		if len(obj.Where) > 0 {
			var w map[string]json.RawMessage
			if err := json.Unmarshal(obj.Where, &w); err == nil {
				keys := make([]string, 0, len(w))
				for k := range w {
					keys = append(keys, k)
				}
				slices.Sort(keys)
				for _, k := range keys {
					r.Conds = append(r.Conds, Condition{Path: k, Mode: "full", Value: w[k]})
				}
			}
		}
		if obj.Type != nil {
			r.Conds = append(r.Conds, Condition{Path: "type", Mode: "full", Value: rawOrNull(mustMarshal(obj.Type))})
		}
	default:
		// a rule is a type name or an object of them, as New API allows; a
		// number, array, bool or the like is a miswritten rule, not an empty
		// one, so it is refused rather than pruned to nothing silently.
		return nil, fmt.Errorf("a prune_objects value is a type name or an object, not a %s", rk.Type)
	}
	return r, nil
}

// prune drops every object below node the rule matches; node itself is kept.
// It gives the pruned node and whether it differs from the input at all — a
// comparison of Go values, so a node that changed is never the same pointer.
func prune(node any, r *pruneRule, ctx Context) (any, bool) {
	keep := func(x any) bool {
		obj, ok := x.(map[string]any)
		if !ok {
			return true
		}
		return !pruneMatch(obj, r, ctx)
	}
	deeper := func(x any) (any, bool) {
		if !r.Rec {
			return x, false
		}
		switch y := x.(type) {
		case map[string]any:
			return prune(y, r, ctx)
		case []any:
			return prune(y, r, ctx)
		}
		return x, false
	}
	switch node := node.(type) {
	case []any:
		out := make([]any, 0, len(node))
		changed := false
		for _, x := range node {
			if !keep(x) {
				changed = true
				continue
			}
			y, d := deeper(x)
			if d {
				changed = true
			}
			out = append(out, y)
		}
		return out, changed
	case map[string]any:
		out := map[string]any{}
		changed := false
		for k, x := range node {
			if !keep(x) {
				changed = true
				continue
			}
			y, d := deeper(x)
			if d {
				changed = true
			}
			out[k] = y
		}
		return out, changed
	}
	return node, false
}

// pruneMatch is the rule's conditions run over one object, with its fields in
// the body's place: the same comparison a condition makes, so a "type"
// condition is a field of the object, as the rule's where is.
func pruneMatch(obj map[string]any, r *pruneRule, ctx Context) bool {
	b, err := json.Marshal(obj)
	if err != nil {
		return false
	}
	and := strings.EqualFold(strings.TrimSpace(r.Logic), "AND")
	for i := range r.Conds {
		hit := conditionHit(b, ctx, &r.Conds[i])
		if and && !hit {
			return false
		}
		if !and && hit {
			return true
		}
	}
	return and
}

// mustMarshal is a small value's JSON, for a rule's built condition.
func mustMarshal(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// parseReturnError reads a return_error's value: a string (its message) or an
// object with message, code, type, status_code and skip_retry. The defaults
// are New API's — 400 and terminate the route — and a status outside 100-599
// is a parse error.
func parseReturnError(v json.RawMessage) (*ReturnError, error) {
	re := &ReturnError{Code: "invalid_request", Type: "invalid_request_error", StatusCode: 400, SkipRetry: true}
	rk := gjson.ParseBytes(v)
	switch {
	case len(v) == 0:
	case rk.Type == gjson.String:
		re.Message = strings.TrimSpace(rk.Str)
	case isJSONObject(rk):
		var obj struct {
			Message    *string `json:"message"`
			Msg        *string `json:"msg"`
			Code       *string `json:"code"`
			Type       *string `json:"type"`
			StatusCode *int    `json:"status_code"`
			Status     *int    `json:"status"`
			SkipRetry  *bool   `json:"skip_retry"`
		}
		if err := json.Unmarshal(v, &obj); err != nil {
			return nil, err
		}
		msg := ""
		if obj.Message != nil {
			msg = *obj.Message
		} else if obj.Msg != nil {
			msg = *obj.Msg
		}
		re.Message = strings.TrimSpace(msg)
		if obj.Code != nil {
			re.Code = *obj.Code
		}
		if obj.Type != nil {
			re.Type = *obj.Type
		}
		if obj.SkipRetry != nil {
			re.SkipRetry = *obj.SkipRetry
		}
		if obj.StatusCode != nil {
			re.StatusCode = *obj.StatusCode
		} else if obj.Status != nil {
			re.StatusCode = *obj.Status
		}
		if re.StatusCode < 100 || re.StatusCode > 599 {
			return nil, fmt.Errorf("return_error's status is from 100 to 599, not %d", re.StatusCode)
		}
	default:
		return nil, fmt.Errorf("return_error's value is a string or an object, not a %s", rk.Type)
	}
	if re.Message == "" {
		re.Message = "turned away by param override"
	}
	return re, nil
}
