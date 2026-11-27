package override

import (
	"encoding/json"
	"strings"
	"testing"
)

// wantInvalid runs Validate and wants it refused, the message naming what it
// refused.
func wantInvalid(t *testing.T, ov, why string) {
	t.Helper()
	_, err := Validate(json.RawMessage(ov))
	if err == nil {
		t.Fatalf("saved %s, want a refusal naming %s", ov, why)
	}
	if !strings.Contains(err.Error(), why) {
		t.Fatalf("err = %v, want it to name %q", err, why)
	}
}

// wantValid runs Validate and wants it accepted.
func wantValid(t *testing.T, ov string) {
	t.Helper()
	if _, err := Validate(json.RawMessage(ov)); err != nil {
		t.Fatalf("rejected %s: %v", ov, err)
	}
}

func TestValidateRootModel(t *testing.T) {
	t.Run("a legacy model key is refused", func(t *testing.T) {
		wantInvalid(t, `{"model":"gpt-5"}`, "model")
	})
	t.Run("a set of the root model is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"path":"model","mode":"set","value":"gpt-5"}]}`, "model")
	})
	t.Run("a delete of the root model is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"path":"model","mode":"delete"}]}`, "model")
	})
	t.Run("a move off the root model is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"path":"m","mode":"move","from":"model","to":"m"}]}`, "model")
	})
	t.Run("a move onto the root model is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"path":"m","mode":"move","from":"m","to":"model"}]}`, "model")
	})
	t.Run("a copy onto the root model is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"path":"m","mode":"copy","from":"m","to":"model"}]}`, "model")
	})
	t.Run("a model_id is not the model", func(t *testing.T) {
		wantValid(t, `{"operations":[{"path":"model_id","mode":"set","value":"x"}]}`)
	})
	t.Run("a nested model path is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"path":"model.x","mode":"set","value":"y"}]}`, "model")
	})
}

func TestValidateModes(t *testing.T) {
	t.Run("an unknown mode is refused by name", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"path":"a","mode":"frobnicate"}]}`, "frobnicate")
	})
	t.Run("a header mode is refused, pointing at the plugin", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"mode":"set_header","value":{}}]}`, "set_header")
	})
	t.Run("an operation with no mode is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"path":"a"}]}`, "no mode")
	})
	t.Run("a bad condition mode is refused by name", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"path":"a","mode":"set","value":1,"conditions":[{"path":"b","mode":"fuzzy"}]}]}`, "fuzzy")
	})
	t.Run("a move without both ends is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"mode":"move","from":"a"}]}`, "from and a to")
	})
	t.Run("a copy without both ends is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"mode":"copy","to":"b"}]}`, "from and a to")
	})
	t.Run("a replace without a from is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"path":"s","mode":"replace"}]}`, "from")
	})
	t.Run("a regex_replace without a from is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"path":"s","mode":"regex_replace"}]}`, "from")
	})
	t.Run("a regex_replace whose pattern does not compile is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"path":"s","mode":"regex_replace","from":"[0-9","to":"x"}]}`, "does not compile")
	})
	t.Run("a valid operation passes", func(t *testing.T) {
		wantValid(t, `{"operations":[{"path":"temperature","mode":"set","value":0.2}]}`)
	})
}

func TestValidateShapes(t *testing.T) {
	t.Run("a set with no path is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"mode":"set","value":1}]}`, "a path is needed")
	})
	t.Run("an unbalanced bracket is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"path":"a.b[","mode":"set","value":1}]}`, "not closed")
	})
	t.Run("a stray close bracket is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"path":"a.b]","mode":"set","value":1}]}`, "no matching [")
	})
	t.Run("a balanced index is fine", func(t *testing.T) {
		wantValid(t, `{"operations":[{"path":"messages.[0]","mode":"set","value":"x"}]}`)
	})
	t.Run("a negative index is fine", func(t *testing.T) {
		wantValid(t, `{"operations":[{"path":"messages.-1","mode":"set","value":"x"}]}`)
	})
	t.Run("a wildcard is fine", func(t *testing.T) {
		wantValid(t, `{"operations":[{"path":"a.*","mode":"to_upper"}]}`)
	})
}

func TestValidatePruneAndReturnError(t *testing.T) {
	t.Run("a prune_objects with no value is empty but valid", func(t *testing.T) {
		// an empty rule prunes nothing; it is saved, and the engine is a no-op
		wantValid(t, `{"operations":[{"path":"o","mode":"prune_objects","value":{}}]}`)
	})
	t.Run("a prune_objects value that is neither a string nor an object is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"path":"o","mode":"prune_objects","value":3}]}`, "prune_objects")
	})
	t.Run("a return_error whose status is out of range is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"mode":"return_error","value":{"message":"x","status_code":700}}]}`, "status")
	})
	t.Run("a return_error value that is neither a string nor an object is refused", func(t *testing.T) {
		wantInvalid(t, `{"operations":[{"mode":"return_error","value":3}]}`, "string or an object")
	})
	t.Run("a valid return_error passes", func(t *testing.T) {
		wantValid(t, `{"operations":[{"mode":"return_error","value":{"message":"gone","status_code":404}}]}`)
	})
}

func TestValidateReportsEveryProblem(t *testing.T) {
	t.Run("two bad operations both show", func(t *testing.T) {
		_, err := Validate(json.RawMessage(`{"operations":[{"path":"model","mode":"set","value":"x"},{"path":"a","mode":"frobnicate"}]}`))
		if err == nil {
			t.Fatal("saved, want a refusal")
		}
		if !strings.Contains(err.Error(), "model") || !strings.Contains(err.Error(), "frobnicate") {
			t.Fatalf("err = %v, want both problems", err)
		}
	})
}
