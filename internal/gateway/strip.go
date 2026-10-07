package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"slices"
	"strings"
	"sync"

	"github.com/yetone/magpie/internal/provider"
	"github.com/yetone/magpie/internal/settings"
)

type stripKey struct{}

type requestStrip struct {
	provider string
	model    string
	paths    []string
	mu       sync.Mutex
	removed  []string
}

func withStripModel(ctx context.Context, pid, model string) (context.Context, *requestStrip) {
	if current, ok := ctx.Value(stripKey{}).(*requestStrip); ok && current.provider == pid && current.model == model {
		return ctx, current
	}
	current := &requestStrip{provider: pid, model: model, paths: provider.StripPathsIn(settings.Load(), pid, model)}
	return context.WithValue(ctx, stripKey{}, current), current
}

func (current *requestStrip) stripped() []string {
	current.mu.Lock()
	defer current.mu.Unlock()
	return slices.Clone(current.removed)
}

func stripOutgoing(ctx context.Context, body []byte) []byte {
	current, ok := ctx.Value(stripKey{}).(*requestStrip)
	if !ok || len(current.paths) == 0 {
		return body
	}
	filtered, removed := withoutPaths(body, current.paths...)
	current.mu.Lock()
	defer current.mu.Unlock()
	for _, path := range removed {
		if !slices.Contains(current.removed, path) {
			current.removed = append(current.removed, path)
		}
	}
	return filtered
}

func withoutPaths(body []byte, paths ...string) ([]byte, []string) {
	if len(paths) == 0 {
		return body, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var root map[string]any
	if decoder.Decode(&root) != nil || root == nil {
		return body, nil
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return body, nil
	}
	var removed []string
	for _, path := range paths {
		clean, err := provider.CleanStripPaths([]string{path})
		if err == nil && stripPath(root, strings.Split(clean[0], ".")) {
			removed = append(removed, clean[0])
		}
	}
	if len(removed) == 0 {
		return body, nil
	}
	filtered, err := json.Marshal(root)
	if err != nil {
		return body, nil
	}
	return filtered, removed
}

func stripPath(node any, segments []string) bool {
	object, ok := node.(map[string]any)
	if !ok {
		return false
	}
	segment := segments[0]
	if field, array := strings.CutSuffix(segment, "[]"); array {
		items, ok := object[field].([]any)
		if !ok || len(segments) == 1 {
			return false
		}
		changed := false
		for _, item := range items {
			changed = stripPath(item, segments[1:]) || changed
		}
		return changed
	}
	child, found := object[segment]
	if !found {
		return false
	}
	if len(segments) == 1 {
		delete(object, segment)
		return true
	}
	if !stripPath(child, segments[1:]) {
		return false
	}
	if nested, ok := child.(map[string]any); ok && len(nested) == 0 {
		delete(object, segment)
	}
	return true
}
