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
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("usage: magpie model strip <provider/model> [path,path|--reset]")
	}
	ref := strings.TrimPrefix(strings.TrimSpace(args[0]), "magpie/")
	reset := isReset(args[1:])
	if reset {
		if _, saved := settings.Load().ModelStrip[ref]; !saved {
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
	if len(args) == 2 {
		var paths []string
		if !reset {
			paths = strings.Split(args[1], ",")
		}
		if err := provider.SetModelStrip(ref, paths); err != nil {
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
	paths := provider.StripPathsIn(settings.Load().ModelStrip, pid, model)
	if len(paths) == 0 {
		fmt.Println(ref + ": no request fields stripped")
	} else {
		fmt.Println(ref + ": strip " + strings.Join(paths, ", "))
		fmt.Println("Provider-wide and model paths are combined. Omitted effort uses the upstream default; it does not disable reasoning.")
	}
	return nil
}

func modelStrips() error {
	all := settings.Load().ModelStrip
	for _, ref := range slices.Sorted(maps.Keys(all)) {
		fmt.Println(ref + ": " + strings.Join(all[ref], ", "))
	}
	if len(all) == 0 {
		fmt.Println("No saved strip rules")
	}
	return nil
}
