package override

import (
	"encoding/json"
	"strings"
)

// Audit lines are the words the Routing page and Recent calls show for the
// operations an override actually ran, one per line, in the order they ran.
// The grammar is New API's: "delete temperature", "set x = y", "move a -> b".

// auditSet is a legacy key's or a set operation's line: the key, and what it
// is set to.
func auditSet(key string, value json.RawMessage) string {
	return "set " + key + " = " + shortValue(value)
}

// auditLine is a non-legacy operation's line, once it changed the body.
func auditLine(op *Operation, body []byte) string {
	switch op.Mode {
	case "delete":
		return "delete " + op.Path
	case "set":
		return "set " + op.Path + " = " + shortValue(op.Value)
	case "move":
		return "move " + op.From + " -> " + op.To
	case "copy":
		return "copy " + op.From + " -> " + op.To
	case "prepend":
		return "prepend " + op.Path
	case "append":
		return "append " + op.Path
	case "replace":
		return "replace " + op.Path + " " + shortJSON(op.From) + " -> " + shortJSON(op.To)
	case "regex_replace":
		return "regex_replace " + op.Path + " " + shortJSON(op.From)
	case "prune_objects":
		p := op.Path
		if p == "" {
			p = "(root)"
		}
		return "prune_objects " + p
	default:
		// trim_prefix, trim_suffix, ensure_prefix, ensure_suffix, trim_space,
		// to_lower, to_upper: the mode and the path it ran on
		return op.Mode + " " + op.Path
	}
}

// shortValue is an operation's value JSON, a long one left at a few bytes:
// the line names the value a set put in, without a body of base64 in it.
func shortValue(v json.RawMessage) string {
	if len(v) == 0 {
		return "null"
	}
	if len(v) > 64 {
		s := string(v)
		return s[:61] + "..."
	}
	return string(v)
}

// shortJSON is a from/to word as a line says it: a short thing as it is, a
// longer one cut.
func shortJSON(s string) string {
	if s == "" {
		return `""`
	}
	if len(s) > 32 {
		return s[:29] + "..."
	}
	return strings.ReplaceAll(s, "\n", "\\n")
}
