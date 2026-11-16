package docstore

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// An overlay over a string: a change is the value itself, "" is no change.
func stringOps() Ops[string, string] {
	return Ops[string, string]{
		Merge: func(base, d string) string {
			if d == "" {
				return base
			}
			return d
		},
		Diff: func(base, v string) string {
			if v == base {
				return ""
			}
			return v
		},
		Count: func(d string) int {
			if d == "" {
				return 0
			}
			return 1
		},
		Parse: func(b []byte) (string, error) {
			var s string
			err := json.Unmarshal(b, &s)
			return s, err
		},
	}
}

// TestOverlay: the base serves with nothing stored; a save stores the difference; the base's own value stores no change but keeps an
// empty document; at the next start a change the base took over is dropped, the document kept.
func TestOverlay(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "scp.db"))
	o := OpenOverlay(t.Context(), db, "o", "file", stringOps(), quiet)
	if o.Value() != "file" || o.Changes() != 0 || o.State().Stored {
		t.Fatalf("fresh: %q %d %+v", o.Value(), o.Changes(), o.State())
	}
	rev, err := o.Save(t.Context(), "mine", "")
	if err != nil || o.Value() != "mine" || o.Changes() != 1 {
		t.Fatalf("a change: %v %q %d", err, o.Value(), o.Changes())
	}
	if _, err := o.Save(t.Context(), "file", rev); err != nil || o.Value() != "file" || o.Changes() != 0 || !o.State().Stored {
		t.Fatalf("back to the file: %v %q %d %+v", err, o.Value(), o.Changes(), o.State())
	}
	if _, err := o.Save(t.Context(), "mine", o.State().Revision); err != nil {
		t.Fatal(err)
	}
	o = OpenOverlay(t.Context(), db, "o", "mine", stringOps(), quiet)
	if o.Value() != "mine" || o.Changes() != 0 || !o.State().Stored {
		t.Errorf("the file took it over: %q %d %+v", o.Value(), o.Changes(), o.State())
	}
	if !Same(map[string]int{"a": 1}, map[string]int{"a": 1}) || Same("a", "b") {
		t.Error("Same")
	}
}
