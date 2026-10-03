package provider

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/yetone/magpie/internal/settings"
)

var stripSegment = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*(\[\])?$`)

func CleanStripPaths(paths []string) ([]string, error) {
	var clean []string
	for _, path := range paths {
		path = strings.TrimSpace(path)
		segments := strings.Split(path, ".")
		for index, segment := range segments {
			if !stripSegment.MatchString(segment) || index == len(segments)-1 && strings.HasSuffix(segment, "[]") {
				return nil, fmt.Errorf("invalid strip path %q: use field, object.field or array[].field", path)
			}
		}
		if segments[0] == "model" || segments[0] == "model[]" {
			return nil, fmt.Errorf("cannot strip model: it identifies the upstream request")
		}
		if !slices.Contains(clean, path) {
			clean = append(clean, path)
		}
	}
	return clean, nil
}

func StripPathsIn(all map[string][]string, pid, model string) []string {
	var paths []string
	for _, key := range []string{pid + "/*", pid + "/" + model} {
		for _, path := range all[key] {
			clean, err := CleanStripPaths([]string{path})
			if err == nil && !slices.Contains(paths, clean[0]) {
				paths = append(paths, clean[0])
			}
		}
	}
	return paths
}

func SetModelStrip(ref string, paths []string) error {
	clean, err := CleanStripPaths(paths)
	if err != nil {
		return err
	}
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "magpie/")
	if strings.HasPrefix(ref, GroupPrefix) {
		return fmt.Errorf("strip paths belong to a provider/model, not a routing group")
	}
	config := settings.Load()
	if len(clean) == 0 {
		if _, found := config.ModelStrip[ref]; !found {
			if current, model, resolveErr := splitRef(ref); resolveErr == nil {
				ref = current.ID + "/" + model
			}
		}
		if err := settings.CheckModelKey("strip paths", ref); err != nil {
			return err
		}
		delete(config.ModelStrip, ref)
		return settings.Save(config)
	}
	current, model, err := splitRef(ref)
	if err != nil {
		return err
	}
	if err := settings.CheckModelKey("strip paths", current.ID+"/"+model); err != nil {
		return err
	}
	if current.asksOwnBackend() {
		return fmt.Errorf("%s uses its own built-in backend; strip paths require a standard API or a plugin", current.Name)
	}
	if model != "*" && !current.serves(model) {
		return fmt.Errorf("%s has no model %s", current.ID, model)
	}
	if config.ModelStrip == nil {
		config.ModelStrip = map[string][]string{}
	}
	config.ModelStrip[current.ID+"/"+model] = clean
	return settings.Save(config)
}
