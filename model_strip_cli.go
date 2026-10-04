package main

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

func modelStrip(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: magpie model strip <provider/model> [path,path|--clear|--reset] [--inherit=true|false]")
	}
	ref := strings.TrimPrefix(strings.TrimSpace(args[0]), "magpie/")
	var paths *[]string
	var inherit *bool
	reset := false
	for _, arg := range args[1:] {
		switch {
		case strings.HasPrefix(arg, "--inherit="):
			value := strings.TrimPrefix(arg, "--inherit=")
			if inherit != nil || value != "true" && value != "false" {
				return fmt.Errorf("use --inherit=true or --inherit=false once")
			}
			v := value == "true"
			inherit = &v
		case arg == "--reset" || arg == "--clear":
			if paths != nil {
				return fmt.Errorf("give paths, --clear or --reset, not more than one")
			}
			paths = new([]string)
			reset = arg == "--reset"
		case strings.HasPrefix(arg, "--"):
			return fmt.Errorf("unknown strip option %q", arg)
		default:
			if paths != nil {
				return fmt.Errorf("give paths as one comma-separated argument")
			}
			v := strings.Split(arg, ",")
			paths = &v
		}
	}
	if reset && inherit != nil {
		return fmt.Errorf("--reset already restores inheritance; do not combine it with --inherit")
	}
	if reset {
		config := settings.Load()
		_, savedPaths := config.ModelStrip[ref]
		_, savedInheritance := config.ModelStripInherit[ref]
		if !savedPaths && !savedInheritance {
			if current, model, err := modelRef(ref); err == nil {
				ref = current.ID + "/" + model
			}
		}
	}
	if !reset {
		current, model, err := modelRef(ref)
		if err != nil {
			return err
		}
		ref = current.ID + "/" + model
	}
	if reset && !strings.HasSuffix(ref, "/*") {
		v := true
		inherit = &v
	}
	if paths != nil || inherit != nil {
		if err := provider.SetStripPreference(ref, paths, inherit); err != nil {
			return err
		}
		if reset {
			fmt.Println(ref + ": own strip entry cleared; provider-wide paths still apply")
		}
	}
	pid, model, err := splitModelRef(ref)
	if err != nil {
		return err
	}
	config := settings.Load()
	effective := provider.StripPathsIn(config, pid, model)
	if model != "*" {
		fmt.Printf("%s: inherit strip %t\n", ref, provider.StripInheritsIn(config, pid, model))
	}
	if len(effective) == 0 {
		fmt.Println(ref + ": no request fields stripped")
	} else {
		fmt.Println(ref + ": strip " + strings.Join(effective, ", "))
		fmt.Println("Omitted effort uses the upstream default; it does not disable reasoning.")
	}
	return nil
}

func modelStrips() error {
	config := settings.Load()
	refs := map[string]bool{}
	for ref := range config.ModelStrip {
		refs[ref] = true
	}
	for ref := range config.ModelStripInherit {
		refs[ref] = true
	}
	for _, ref := range slices.Sorted(maps.Keys(refs)) {
		line := ref + ": " + strings.Join(config.ModelStrip[ref], ", ")
		if inherit, set := config.ModelStripInherit[ref]; set && !inherit {
			line += " (inherit strip false)"
		}
		fmt.Println(line)
	}
	if len(refs) == 0 {
		fmt.Println("No saved strip rules")
	}
	return nil
}
