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

func StripInheritsIn(config settings.Settings, pid, model string) bool {
	inherit, set := config.ModelStripInherit[pid+"/"+model]
	return !set || inherit
}

func StripPathsIn(config settings.Settings, pid, model string) []string {
	var paths []string
	keys := []string{pid + "/" + model}
	if model != "*" && StripInheritsIn(config, pid, model) {
		keys = append([]string{pid + "/*"}, keys...)
	}
	for _, key := range keys {
		for _, path := range config.ModelStrip[key] {
			clean, err := CleanStripPaths([]string{path})
			if err == nil && !slices.Contains(paths, clean[0]) {
				paths = append(paths, clean[0])
			}
		}
	}
	return paths
}

func SetModelStrip(ref string, paths []string) error {
	return SetStripPreference(ref, &paths, nil)
}

// SetStripPreference updates paths and inheritance together. Nil keeps that
// part, empty paths clear only the list, and inherit=true restores inheritance.
func SetStripPreference(ref string, paths *[]string, inherit *bool) error {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "magpie/")
	if strings.HasPrefix(ref, GroupPrefix) {
		return fmt.Errorf("strip paths belong to a provider/model, not a routing group")
	}
	config := settings.Load()
	_, savedPaths := config.ModelStrip[ref]
	_, savedInheritance := config.ModelStripInherit[ref]
	clearing := paths != nil && len(*paths) == 0 && (inherit == nil || *inherit)
	if clearing && (savedPaths || savedInheritance) {
		// An exact saved key wins over another provider now using its name.
		if err := settings.CheckModelKey("strip paths", ref); err != nil {
			return err
		}
		if inherit != nil && strings.HasSuffix(ref, "/*") {
			return fmt.Errorf("strip inheritance belongs to a model, not a provider")
		}
		applyStripPreference(&config, ref, paths, inherit)
		return settings.Save(config)
	}
	current, model, err := splitRef(ref)
	if err != nil {
		// A removed provider's preferences must still be resettable.
		if clearing {
			if checkErr := settings.CheckModelKey("strip paths", ref); checkErr != nil {
				return checkErr
			}
			if inherit != nil && strings.HasSuffix(ref, "/*") {
				return fmt.Errorf("strip inheritance belongs to a model, not a provider")
			}
			applyStripPreference(&config, ref, paths, inherit)
			return settings.Save(config)
		}
		return err
	}
	if err := checkStripPreference(*current, model, paths, inherit); err != nil {
		return err
	}
	applyStripPreference(&config, current.ID+"/"+model, paths, inherit)
	return settings.Save(config)
}

func (p Provider) SupportsStrip() bool { return !p.asksOwnBackend() }

func checkStripPreference(p Provider, model string, paths *[]string, inherit *bool) error {
	if err := settings.CheckModelKey("strip paths", p.ID+"/"+model); err != nil {
		return err
	}
	if inherit != nil && model == "*" {
		return fmt.Errorf("strip inheritance belongs to a model, not a provider")
	}
	if paths != nil {
		if _, err := CleanStripPaths(*paths); err != nil {
			return err
		}
	}
	active := paths != nil && len(*paths) > 0 || inherit != nil && !*inherit
	if active && !p.SupportsStrip() {
		return fmt.Errorf("%s uses its own built-in backend; strip paths require a standard API or a plugin", p.Name)
	}
	if active && model != "*" && !slices.Contains(p.Models, model) && !p.serves(model) {
		return fmt.Errorf("%s has no model %s", p.ID, model)
	}
	return nil
}

// CheckStripPrefs is also used before the editor writes the provider itself.
func CheckStripPrefs(p Provider, paths *[]string, prefs map[string]ModelPref) error {
	if paths != nil {
		if err := checkStripPreference(p, "*", paths, nil); err != nil {
			return err
		}
	}
	for model, pref := range prefs {
		if pref.Strip != nil || pref.InheritStrip != nil {
			if err := checkStripPreference(p, model, pref.Strip, pref.InheritStrip); err != nil {
				return err
			}
		}
	}
	return nil
}

func applyStripPreference(config *settings.Settings, ref string, paths *[]string, inherit *bool) {
	if paths != nil {
		clean, _ := CleanStripPaths(*paths) // validated before any preference is changed
		if len(clean) == 0 {
			delete(config.ModelStrip, ref)
		} else {
			if config.ModelStrip == nil {
				config.ModelStrip = map[string][]string{}
			}
			config.ModelStrip[ref] = clean
		}
	}
	if inherit != nil {
		if *inherit {
			delete(config.ModelStripInherit, ref)
		} else {
			if config.ModelStripInherit == nil {
				config.ModelStripInherit = map[string]bool{}
			}
			config.ModelStripInherit[ref] = false
		}
	}
}

func setStripPrefs(p Provider, paths *[]string, prefs map[string]ModelPref) error {
	if err := CheckStripPrefs(p, paths, prefs); err != nil {
		return err
	}
	config := settings.Load()
	changed := paths != nil
	applyStripPreference(&config, p.ID+"/*", paths, nil)
	for model, pref := range prefs {
		changed = changed || pref.Strip != nil || pref.InheritStrip != nil
		applyStripPreference(&config, p.ID+"/"+model, pref.Strip, pref.InheritStrip)
	}
	if !changed {
		return nil
	}
	return settings.Save(config)
}

func copyStripPrefs(from, to string) error {
	config := settings.Load()
	changed := false
	for ref, paths := range config.ModelStrip {
		if model, ok := strings.CutPrefix(ref, from+"/"); ok {
			config.ModelStrip[to+"/"+model] = slices.Clone(paths)
			changed = true
		}
	}
	for ref, inherit := range config.ModelStripInherit {
		if model, ok := strings.CutPrefix(ref, from+"/"); ok {
			config.ModelStripInherit[to+"/"+model] = inherit
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return settings.Save(config)
}
