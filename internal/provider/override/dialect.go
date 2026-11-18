// Package override applies a New API param_override to an outbound request
// body at the wire's last moment.
//
// The JSON accepted here is New API's param_override dialect, verbatim: a
// top-level map whose keys are set as-is (the legacy form) and whose
// "operations" entry is a list of {path, mode, value, ...} run in order, the
// legacy keys first. Paths are gjson paths; conditions match the body and a
// small request-identity context. The body-side modes are set, delete, move,
// copy, prepend, append, trim_prefix, trim_suffix, ensure_prefix,
// ensure_suffix, trim_space, to_lower, to_upper, replace, regex_replace,
// prune_objects and return_error.
//
// The dialect and the behavioral expectations are New API's (参数覆盖,
// QuantumNous/new-api, AGPL-3.0 — its code is not copied). The logic is a
// fresh port of the MIT-licensed community plugin
// @magpie-community/middleware-param-override, which is itself a port of the
// same feature to magpie's agent-level middleware point; here it runs at the
// final outbound body instead, so it also reaches the fields magpie's own
// protocol translation and wire tier write. Header modes (set_header,
// pass_headers, sync_fields, ...) are unsupported here — that point has no
// header-override context — and are refused as unknown, as New API does.
package override

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// BodyModes are the operation modes this engine runs on the request body.
// Anything else — the header modes and anything unrecognized — is refused,
// at save time and at run time alike, as New API refuses an unknown mode.
var BodyModes = map[string]bool{
	"set":           true,
	"delete":        true,
	"move":          true,
	"copy":          true,
	"prepend":       true,
	"append":        true,
	"trim_prefix":   true,
	"trim_suffix":   true,
	"ensure_prefix": true,
	"ensure_suffix": true,
	"trim_space":    true,
	"to_lower":      true,
	"to_upper":      true,
	"replace":       true,
	"regex_replace": true,
	"prune_objects": true,
	"return_error":  true,
}

// ConditionModes are the ways a condition compares its value.
var ConditionModes = map[string]bool{
	"full":     true,
	"prefix":   true,
	"suffix":   true,
	"contains": true,
	"regex":    true,
	"gt":       true,
	"gte":      true,
	"lt":       true,
	"lte":      true,
}

// Condition is one way of asking whether an operation should run. Its path is
// looked up in the body first and then in the request context; Mode is one of
// ConditionModes (empty is full); Invert flips the answer; PassMissingKey lets
// the condition pass when neither the body nor the context has its path.
type Condition struct {
	Path           string          `json:"path"`
	Mode           string          `json:"mode"`
	Value          json.RawMessage `json:"value"`
	Invert         bool            `json:"invert"`
	PassMissingKey bool            `json:"pass_missing_key"`
}

// Operation is one param_override operation, in the order its JSON gives.
// Value is the raw JSON the operation acts with; From and To are move's and
// copy's source and destination paths; Logic is AND or OR (empty is OR);
// KeepOrigin is honored by set, prepend and append.
type Operation struct {
	Path       string          `json:"path"`
	Mode       string          `json:"mode"`
	Value      json.RawMessage `json:"value"`
	KeepOrigin bool            `json:"keep_origin"`
	From       string          `json:"from"`
	To         string          `json:"to"`
	Conditions []Condition     `json:"conditions"`
	Logic      string          `json:"logic"`
}

// KeyValue is one of the override's legacy top-level keys: the key is set in
// the body as-is, Value its raw JSON.
type KeyValue struct {
	Key   string
	Value json.RawMessage
}

// Parsed is an override read into the form the engine and Validate both run
// on: the legacy keys, in sorted key order, and the operations, in their own.
type Parsed struct {
	Legacy []KeyValue
	Ops    []Operation
	// Empty is whether the override changes nothing: no legacy key and no
	// operation. Apply fast-paths on it without decoding the body.
	Empty bool
}

// ErrUnknownMode is an operation mode this engine does not run (a header mode
// or an unrecognized one). It names the mode and points at the agent-level
// plugin for the header ones.
type ErrUnknownMode struct{ Mode string }

func (e ErrUnknownMode) Error() string {
	if e.Mode == "" {
		return "an operation with no mode is not a param override operation"
	}
	if headerModes[e.Mode] {
		return "the mode " + e.Mode + " changes headers, and the wire has no header-override here to act on; set headers pre-routing with the param-override plugin"
	}
	return "the mode " + e.Mode + " is not a param override mode (the body modes are " + strings.Join(bodyModeNames, ", ") + ")"
}

// ErrNotList is a param_override whose operations entry, or an operation's
// conditions, is not a list (conditions may also be an object of key: value).
type ErrNotList struct{ What string }

func (e ErrNotList) Error() string { return e.What + " is not a JSON list" }

// ErrNotObject is a param_override whose top level is not a JSON object.
type ErrNotObject struct{ Err error }

func (e ErrNotObject) Error() string { return "a param override is a JSON object: " + e.Err.Error() }

// headerModes are New API's header modes, refused here with a pointer to the
// agent-level plugin, apart from the plain unknown-mode message.
var headerModes = map[string]bool{
	"set_header":    true,
	"delete_header": true,
	"copy_header":   true,
	"move_header":   true,
	"pass_headers":  true,
	"sync_fields":   true,
}

var bodyModeNames = []string{
	"set", "delete", "move", "copy", "prepend", "append",
	"trim_prefix", "trim_suffix", "ensure_prefix", "ensure_suffix",
	"trim_space", "to_lower", "to_upper", "replace", "regex_replace",
	"prune_objects", "return_error",
}

// Parse reads an override's JSON into its Parsed form. The legacy top-level
// keys keep their sorted key order for determinism; the operations keep their
// own. An override with no legacy key and no operation is Empty.
func Parse(raw json.RawMessage) (*Parsed, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, ErrNotObject{Err: err}
	}
	p := &Parsed{Empty: true}
	for k, v := range top {
		if k == "operations" {
			continue
		}
		p.Legacy = append(p.Legacy, KeyValue{Key: k, Value: json.RawMessage(v)})
		p.Empty = false
	}
	sort.Slice(p.Legacy, func(i, j int) bool { return p.Legacy[i].Key < p.Legacy[j].Key })
	if rawOps, ok := top["operations"]; ok {
		var rawList []json.RawMessage
		if err := json.Unmarshal(rawOps, &rawList); err != nil {
			return nil, ErrNotList{What: "operations"}
		}
		p.Ops = make([]Operation, 0, len(rawList))
		for i, r := range rawList {
			op, err := parseOperation(r)
			if err != nil {
				return nil, fmt.Errorf("operations[%d]: %w", i, err)
			}
			p.Ops = append(p.Ops, op)
		}
		if len(p.Ops) > 0 {
			p.Empty = false
		}
	}
	return p, nil
}

// parseOperation reads one operation's raw JSON. conditions is read on its
// own: a list of {path, mode, value, ...} or an object of key: value, each key
// asked full against its value.
func parseOperation(r json.RawMessage) (Operation, error) {
	var op Operation
	if err := json.Unmarshal(r, &op); err != nil {
		// conditions in the object form is the usual cause: re-read the raw
		// operation without conditions and read them by hand.
		var plain struct {
			Path       string          `json:"path"`
			Mode       string          `json:"mode"`
			Value      json.RawMessage `json:"value"`
			KeepOrigin bool            `json:"keep_origin"`
			From       string          `json:"from"`
			To         string          `json:"to"`
			Logic      string          `json:"logic"`
		}
		if perr := json.Unmarshal(r, &plain); perr != nil {
			return Operation{}, err
		}
		op = Operation{Path: plain.Path, Mode: plain.Mode, Value: plain.Value, KeepOrigin: plain.KeepOrigin, From: plain.From, To: plain.To, Logic: plain.Logic}
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(r, &raw); err != nil {
		return Operation{}, err
	}
	if c, ok := raw["conditions"]; ok {
		conds, err := parseConditions(c)
		if err != nil {
			return Operation{}, err
		}
		op.Conditions = conds
	}
	return op, nil
}

// parseConditions reads an operation's conditions: a list of condition
// objects, or an object of key: value, each key asked full against its value.
func parseConditions(c json.RawMessage) ([]Condition, error) {
	var list []Condition
	if err := json.Unmarshal(c, &list); err == nil {
		return list, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(c, &obj); err != nil {
		return nil, ErrNotList{What: "conditions is a list or an object of key: value"}
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	conds := make([]Condition, 0, len(obj))
	for _, k := range keys {
		conds = append(conds, Condition{Path: k, Mode: "full", Value: json.RawMessage(obj[k])})
	}
	return conds, nil
}
