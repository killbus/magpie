package cli

// The param override, from the terminal: the New API param_override JSON a
// model's requests are adjusted by at the wire (provider.SetModelOverride).
// One JSON, the whole override: there is no separate command for deleting a
// field, and a field is deleted with the standard "delete" operation.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/provider/override"
	"github.com/yetone/magpie/internal/settings"
)

// modelOverride is `magpie model override <ref> ['<json>']`: with no JSON it
// shows the override in force, with one it saves it, --reset/--clear takes it
// away, and --inherit/--no-inherit keeps the model out of (or back in) its
// provider's '<provider>/*' override.
func modelOverride(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", modelUsage)
	}
	if slices.Contains([]string{"-h", "--help", "help"}, args[0]) {
		fmt.Println(modelUsage)
		return nil
	}
	ref := args[0]
	rest := args[1:]
	if len(rest) == 1 && slices.Contains([]string{"--reset", "--clear", "-r", "reset", "clear"}, rest[0]) {
		dropped, err := provider.DropModelOverride(ref)
		if err != nil {
			return err
		}
		if !dropped {
			fmt.Println(muted.Render(ref + " had no param override"))
			return nil
		}
		fmt.Println(green.Render("✓"), ref, muted.Render("· its requests go as magpie builds them again"))
		return nil
	}
	// the inherit switch: kept out of the provider's '<provider>/*' override
	// (its own, if it has one, still applies), or back in
	if len(rest) == 1 && slices.Contains([]string{"--inherit", "--no-inherit", "inherit", "no-inherit"}, rest[0]) {
		out := rest[0] == "--no-inherit" || rest[0] == "no-inherit"
		if err := provider.SetModelOverrideInherit(ref, out); err != nil {
			return err
		}
		if out {
			fmt.Println(green.Render("✓"), ref, muted.Render("· kept out of "+strings.Split(ref, "/")[0]+"/*; its requests go as magpie builds them"))
		} else {
			fmt.Println(green.Render("✓"), ref, muted.Render("· inherits its provider's override again"))
		}
		return nil
	}
	if len(rest) == 0 {
		return showModelOverride(ref)
	}
	if len(rest) > 1 {
		return fmt.Errorf("give the override as one JSON value, quoted: magpie model override <provider/model> '<json>'")
	}
	raw := json.RawMessage(strings.TrimSpace(rest[0]))
	if len(raw) == 0 || !json.Valid(raw) {
		return fmt.Errorf("the param override is not valid JSON: %s", rest[0])
	}
	// Validate before the save, so the error names what is wrong the way the
	// engine's own save-time rules do; SetModelOverride validates again and
	// refuses the root model and an unknown mode alike
	if _, err := override.Validate(raw); err != nil {
		return err
	}
	if err := provider.SetModelOverride(ref, raw); err != nil {
		return err
	}
	fmt.Println(green.Render("✓"), ref, muted.Render("· its requests are adjusted by this param override"))
	return nil
}

// showModelOverride prints the override in force for ref: the model's own, or
// its provider's unless the model opted out, or nothing; it says which. The
// order is ModelOverrideOf's: a model's own wins, then its opt-out, then the
// provider's.
func showModelOverride(ref string) error {
	p, model, err := modelRef(ref)
	if err != nil {
		return err
	}
	pid := p.ID
	s := settings.Load()
	own, hasOwn := s.ModelOverride[pid+"/"+model]
	every, hasEvery := s.ModelOverride[pid+"/*"]
	optedOut, explicit := provider.ModelOverrideInherit(pid, model)
	if hasOwn {
		fmt.Println(bold.Render(pid+"/"+model), muted.Render("· its param override"))
		fmt.Println(indentJSON(own))
		if explicit && optedOut && hasEvery {
			fmt.Println(muted.Render("  · kept out of " + pid + "/*, which does not apply to it"))
		}
		return nil
	}
	if explicit && optedOut {
		fmt.Println(bold.Render(pid+"/"+model), muted.Render("· opted out of "+pid+"/*; its requests go as magpie builds them"))
		return nil
	}
	if hasEvery {
		fmt.Println(bold.Render(pid+"/"+model), muted.Render("· from "+pid+"/* (every model of "+p.Name+")"))
		fmt.Println(indentJSON(every))
		return nil
	}
	fmt.Println(bold.Render(pid+"/"+model), muted.Render("· no param override · magpie model override "+pid+"/"+model+" '<json>' to give it one"))
	return nil
}

// indentJSON is a stored override shown pretty-printed, as it reads; a value
// that does not parse is shown as it is.
func indentJSON(raw json.RawMessage) string {
	var out bytes.Buffer
	if err := json.Indent(&out, raw, "  ", "  "); err != nil {
		return "  " + string(raw)
	}
	return "  " + strings.ReplaceAll(out.String(), "\n", "\n  ")
}

// modelOverrides is `magpie model overrides`: the overrides the user set,
// each model's own and each provider's *, with the models that opted out.
func modelOverrides() error {
	s := settings.Load()
	keys := slices.Sorted(maps.Keys(s.ModelOverride))
	if len(keys) == 0 {
		fmt.Println(muted.Render("no model has a param override yet · magpie model override <provider/model> '<json>'"))
		return nil
	}
	w := 0
	for _, k := range keys {
		w = max(w, len(k))
	}
	for _, k := range keys {
		note := ""
		if strings.HasSuffix(k, "/*") {
			note = muted.Render(" · every model of the provider")
		}
		fmt.Println("  " + pad(k, w) + "  " + compactJSON(s.ModelOverride[k]) + note)
	}
	// the models kept out of a provider's override, apart: they have no
	// override of their own, and saying so here is what the list would
	// otherwise leave out
	var outs []string
	for k, v := range s.ModelOverrideInherit {
		if !v {
			outs = append(outs, k)
		}
	}
	slices.Sort(outs)
	if len(outs) > 0 {
		fmt.Println()
		for _, k := range outs {
			fmt.Println("  " + pad(k, w) + "  " + muted.Render("opted out · sent as magpie builds it"))
		}
	}
	return nil
}

// compactJSON is an override on one line for the list.
func compactJSON(raw json.RawMessage) string {
	var out bytes.Buffer
	if err := json.Compact(&out, raw); err != nil {
		return string(raw)
	}
	return out.String()
}
