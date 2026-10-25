package override

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Validate runs the save-time checks on an override's raw JSON, and gives the
// parsed override on success so the caller saves exactly what it validated.
// It reports every problem at once, in the order a CLI or GUI can print them:
// a root-model target, an unknown or missing mode, a condition mode this
// engine does not compare, a path whose brackets are unbalanced or that is
// missing where a mode needs one, a move/copy without both its ends, a
// replace without its from, a regex_replace whose pattern does not compile,
// a return_error whose status is out of range, or a prune_objects value that
// is not a rule. The checks are New API's save-time refusals, re-expressed:
// the root model field is the one path protected, because it names the
// upstream request.
func Validate(raw json.RawMessage) (*Parsed, error) {
	p, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	var problems []string
	report := func(what string) { problems = append(problems, what) }

	// a legacy top-level key is a literal set, and the only one the dialect
	// protects is the root model, by its exact name.
	for _, kv := range p.Legacy {
		if kv.Key == "model" {
			report("a legacy key of model sets the root model field, which is protected")
		}
	}

	for i := range p.Ops {
		op := &p.Ops[i]
		where := "operations[" + strconv.Itoa(i) + "]"
		mode := strings.ToLower(strings.TrimSpace(op.Mode))
		if !BodyModes[mode] {
			report(where + ": " + ErrUnknownMode{Mode: op.Mode}.Error())
			continue
		}
		for j := range op.Conditions {
			c := &op.Conditions[j]
			cwhere := where + ".conditions[" + strconv.Itoa(j) + "]"
			if c.Mode != "" {
				cm := strings.ToLower(strings.TrimSpace(c.Mode))
				if !ConditionModes[cm] {
					report(cwhere + ": the condition mode " + quote(c.Mode) + " is not one of " + strings.Join(conditionModeNames, ", "))
				}
			}
			if err := pathShape(c.Path, true); err != nil {
				report(cwhere + ": " + err.Error())
			}
		}
		switch mode {
		case "move":
			if op.From == "" || op.To == "" {
				report(where + ": a move needs both a from and a to")
			} else {
				if rootModelPath(op.From) {
					report(where + ": a move cannot take the root model field, which is protected")
				}
				if rootModelPath(op.To) {
					report(where + ": a move cannot put a value in the root model field, which is protected")
				}
				if err := pathShape(op.From, true); err != nil {
					report(where + ".from: " + err.Error())
				}
				if err := pathShape(op.To, true); err != nil {
					report(where + ".to: " + err.Error())
				}
			}
		case "copy":
			if op.From == "" || op.To == "" {
				report(where + ": a copy needs both a from and a to")
			} else {
				if rootModelPath(op.To) {
					report(where + ": a copy cannot put a value in the root model field, which is protected")
				}
				if err := pathShape(op.From, true); err != nil {
					report(where + ".from: " + err.Error())
				}
				if err := pathShape(op.To, true); err != nil {
					report(where + ".to: " + err.Error())
				}
			}
		case "replace":
			if op.From == "" {
				report(where + ": a replace needs a from")
			}
			if err := pathShape(op.Path, true); err != nil {
				report(where + ": " + err.Error())
			}
		case "regex_replace":
			if op.From == "" {
				report(where + ": a regex_replace needs a from")
			} else if _, e := regexp.Compile(op.From); e != nil {
				report(where + ": the regex_replace pattern does not compile: " + e.Error())
			}
			if err := pathShape(op.Path, true); err != nil {
				report(where + ": " + err.Error())
			}
		case "prune_objects":
			// the path may be empty, the root; the value must read as a rule
			if _, e := parsePruneRule(op.Value); e != nil {
				report(where + ": " + e.Error())
			}
			if err := pathShape(op.Path, false); err != nil {
				report(where + ": " + err.Error())
			}
		case "return_error":
			if _, e := parseReturnError(op.Value); e != nil {
				report(where + ": " + e.Error())
			}
		default:
			// set, delete, prepend, append, the trim and to_ modes: they act on
			// their path, and the only one of those paths the dialect protects
			// is the root model.
			if rootModelPath(op.Path) {
				report(where + ": a " + mode + " of the root model field is protected and cannot be saved")
			}
			if err := pathShape(op.Path, true); err != nil {
				report(where + ": " + err.Error())
			}
		}
	}

	if len(problems) > 0 {
		return nil, fmt.Errorf("param override: %s", strings.Join(problems, "; "))
	}
	return p, nil
}

// rootModelPath is whether a gjson path names the root model field: "model"
// itself, or a path under it, "model.x" or "model[0]". "model_id" is a
// different field and is not protected.
func rootModelPath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "model" {
		return true
	}
	return strings.HasPrefix(p, "model.") || strings.HasPrefix(p, "model[")
}

// pathShape is a gjson path's save-time check: a path a mode needs is not
// empty, and its brackets are balanced. gjson reads a path it does not
// understand as a literal key, so the only malformed shape a save can catch
// is an empty where one is needed, or an unbalanced bracket.
func pathShape(p string, needed bool) error {
	if p == "" {
		if needed {
			return errors.New("a path is needed and none is given")
		}
		return nil
	}
	depth := 0
	for i := 0; i < len(p); i++ {
		switch p[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth < 0 {
				return errors.New("a ] in the path has no matching [")
			}
		}
	}
	if depth > 0 {
		return errors.New("a [ in the path is not closed")
	}
	return nil
}

// quote is a word for an error, in quotes where it has none.
func quote(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "(none)"
	}
	return `"` + s + `"`
}

var conditionModeNames = []string{
	"full", "prefix", "suffix", "contains", "regex", "gt", "gte", "lt", "lte",
}
