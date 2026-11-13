package provider

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/yetone/magpie/internal/provider/override"
	"github.com/yetone/magpie/internal/settings"
)

// A model's param override is the New API param_override JSON the user gave
// it: the fields magpie's own protocol translation and wire tier write, and
// the vendor's request otherwise, are adjusted at the final outbound body by
// the provider/override engine (gateway's wire hook). It is stored in
// settings' ModelOverride by "<provider id>/<model id>" — or
// "<provider id>/*" for every model of that provider — the way the names,
// the reply limits and the image answers are, so a provider rename, a copy
// and a settings page that saves beside it all reach it by the per-model
// rule and nothing here says so by hand.
//
// A model may keep the body as it is even where its provider has an
// override, by settings' ModelOverrideInherit: an explicit opt-out, missing
// otherwise, so a model configured before the field existed inherits, as
// New API's own inheritance does.

// ModelOverrideOf is the override a request for pid's model is run under:
// the model's own, else its provider's, absent when the model opted out or
// neither has one. The settings are the caller's, so the gateway reads them
// once a request and the override is looked up from them without a second
// read.
func ModelOverrideOf(s settings.Settings, pid, model string) (json.RawMessage, bool) {
	key := pid + "/" + model
	// the model's own wins over anything: an opt-out is about inheriting
	// its provider's override, not about throwing away its own
	if raw, ok := s.ModelOverride[key]; ok {
		return raw, true
	}
	// an explicit opt-out keeps the body as it is: the provider's override
	// is not run for this model
	if inherit, ok := s.ModelOverrideInherit[key]; ok && !inherit {
		return nil, false
	}
	if raw, ok := s.ModelOverride[pid+"/*"]; ok {
		return raw, true
	}
	return nil, false
}

// ModelOverride is ModelOverrideOf over the settings read at the call.
func ModelOverride(pid, model string) (json.RawMessage, bool) {
	return ModelOverrideOf(heldSettings(), pid, model)
}

// ModelOverrideInherit says whether the user opted pid's model out of its
// override: true where it is kept as it is, and whether that is an explicit
// choice (ModelOverrideInherit set it) rather than the absence of an
// override to opt out of, which the caller tells apart.
func ModelOverrideInherit(pid, model string) (optedOut, explicit bool) {
	v, ok := heldSettings().ModelOverrideInherit[pid+"/"+model]
	if !ok {
		return false, false
	}
	return !v, true
}

// SetModelOverride saves the param_override JSON a request for a provider's
// model is run under, spelt "provider/model" or "provider/*" for every
// model of it; an empty JSON takes it away, as does DropModelOverride. The
// JSON is checked by the engine's save-time rules first — the root model
// field protected, the modes known — so the gateway is never left with one
// it would refuse at the wire. Like a price, it is not told to the agents:
// what a request is adjusted by is not what a model list shows.
func SetModelOverride(ref string, raw json.RawMessage) error {
	p, model, err := splitRef(ref)
	if err != nil {
		return err
	}
	if model != "*" && !p.serves(model) {
		return fmt.Errorf("%s has no model %s (magpie provider %s lists them)", p.ID, model, p.ID)
	}
	if len(raw) > 0 {
		if _, err := override.Validate(raw); err != nil {
			return err
		}
	}
	s := settings.Load()
	key := p.ID + "/" + model
	if len(raw) == 0 {
		return dropModelOverrideIn(&s, key)
	}
	if cur, ok := s.ModelOverride[key]; ok && bytes.Equal(cur, raw) {
		return nil
	}
	if s.ModelOverride == nil {
		s.ModelOverride = map[string]json.RawMessage{}
	}
	s.ModelOverride[key] = append(json.RawMessage{}, raw...)
	return settings.Save(s)
}

// dropModelOverrideIn takes pid/model's override from s. It leaves an opt-out
// standing: a model kept from its provider's override for a reason of its own
// keeps that, so taking its own override away does not quietly put the
// provider's back in force.
func dropModelOverrideIn(s *settings.Settings, key string) error {
	changed := false
	if _, ok := s.ModelOverride[key]; ok {
		delete(s.ModelOverride, key)
		changed = true
	}
	if !changed {
		return nil
	}
	return settings.Save(*s)
}

// DropModelOverride takes a provider's model's param override, spelt the way
// SetModelOverride spells it, and says whether one was there.
func DropModelOverride(ref string) (bool, error) {
	p, model, err := splitRef(ref)
	if err != nil {
		return false, err
	}
	s := settings.Load()
	key := p.ID + "/" + model
	if _, in := s.ModelOverride[key]; !in {
		return false, nil
	}
	if err := dropModelOverrideIn(&s, key); err != nil {
		return false, err
	}
	return true, nil
}

// SetModelOverrideInherit says whether a provider's model keeps the body as
// it is even where its provider has an override: off (true) opts the model
// out, keeping it from its provider's "<pid>/*" override; false puts it back
// with the override it would have anyway, and the choice is then nothing to
// keep, so it is taken away. The provider's own override is left as it is:
// opting one model out does not take it from the provider's others.
func SetModelOverrideInherit(ref string, off bool) error {
	p, model, err := splitRef(ref)
	if err != nil {
		return err
	}
	// an opt-out is kept for a model this provider serves, as an override is:
	// a key nothing serves is one the editor and the list would never show
	if model != "*" && !p.serves(model) {
		return fmt.Errorf("%s doesn't serve %s", p.Name, model)
	}
	s := settings.Load()
	key := p.ID + "/" + model
	if !off {
		if _, ok := s.ModelOverrideInherit[key]; !ok {
			return nil
		}
		delete(s.ModelOverrideInherit, key)
	} else {
		if v, ok := s.ModelOverrideInherit[key]; ok && !v {
			return nil
		}
		if s.ModelOverrideInherit == nil {
			s.ModelOverrideInherit = map[string]bool{}
		}
		s.ModelOverrideInherit[key] = false
	}
	return settings.Save(s)
}

// ModelOverrides is what the user gave the provider's models, by "<provider
// id>/<model id>" and "<provider id>/*", for the editor to show and save
// back; a provider that has none gives one empty, as a name list does.
func (p Provider) ModelOverrides() map[string]json.RawMessage {
	out := map[string]json.RawMessage{}
	for k, v := range settings.Load().ModelOverride {
		if m, ok := strings.CutPrefix(k, p.ID+"/"); ok {
			out[m] = v
		}
	}
	return out
}

// ModelOverrideInherits is which of the provider's models the user kept out
// of its override, by model id, for the editor's opt-out marks.
func (p Provider) ModelOverrideInherits() map[string]bool {
	out := map[string]bool{}
	for k, v := range settings.Load().ModelOverrideInherit {
		if m, ok := strings.CutPrefix(k, p.ID+"/"); ok {
			out[m] = v
		}
	}
	return out
}
