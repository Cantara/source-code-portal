package docstore

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
)

// Ops are what a feature tells an Overlay about its value T and its stored changes D (design/config-baseline.md).
type Ops[T, D any] struct {
	Merge func(base T, d D) T // the value in force: base with the changes d
	Diff  func(base, v T) D   // the changes that turn base into v; Count is 0 when v is base
	Count func(d D) int       // how many changes d holds
	Parse func([]byte) (D, error)
}

// Overlay is a value whose base comes from the configuration and whose changes, made by admins, are a document in the store: the
// value in force is the base with the changes. What is stored is always the difference from the base, so a value equal to the base
// stores no change. At Open, changes the base has since taken over are dropped: a configuration downloaded and deployed leaves the
// store without changes of its own. An overlay without changes keeps an empty document rather than none, so what is imported only
// into a store that lacks the document (an older release's files or documents) is never imported again.
type Overlay[T, D any] struct {
	doc  *Doc[D]
	base T
	ops  Ops[T, D]
	cur  atomic.Pointer[T]
}

// OpenOverlay reads the changes called name from db (nil: the base alone, and nothing can be saved) and drops the ones equal to base.
func OpenOverlay[T, D any](ctx context.Context, db Backend, name string, base T, ops Ops[T, D], log *slog.Logger) *Overlay[T, D] {
	var none D
	o := &Overlay[T, D]{doc: Open(ctx, db, name, none, ops.Parse, log), base: base, ops: ops}
	if st := o.doc.Get(); st.Stored && st.Problem == "" {
		pruned := ops.Diff(base, ops.Merge(base, st.Value))
		if b, err := Encode(pruned); err != nil {
			log.Error("the changes could not be encoded", "document", name, "err", err)
		} else if was, _ := Encode(st.Value); !bytes.Equal(b, was) {
			if _, err := o.doc.Save(ctx, b, st.Revision); err != nil {
				log.Error("the changes equal to the configuration were not dropped", "document", name, "err", err)
			} else {
				log.Info("changes equal to the configuration dropped from the store", "document", name, "kept", ops.Count(pruned))
			}
		}
	}
	o.refresh()
	return o
}

func (o *Overlay[T, D]) refresh() {
	v := o.ops.Merge(o.base, o.doc.Get().Value)
	o.cur.Store(&v)
}

// Value is the value in force.
func (o *Overlay[T, D]) Value() T { return *o.cur.Load() }

// Base is the configuration's value.
func (o *Overlay[T, D]) Base() T { return o.base }

// State is the stored changes: their revision, whether any are stored and why they are not served.
func (o *Overlay[T, D]) State() State[D] { return o.doc.Get() }

// Changes is how many changes are kept only in the store; 0 when the stored document is not served.
func (o *Overlay[T, D]) Changes() int {
	if st := o.doc.Get(); st.Stored && st.Problem == "" {
		return o.ops.Count(st.Value)
	}
	return 0
}

// Writable reports whether changes can be saved.
func (o *Overlay[T, D]) Writable() bool { return o.doc.Writable() }

// Save stores v's difference from the base when revision is the stored one (empty: nothing stored yet) and returns the new revision.
func (o *Overlay[T, D]) Save(ctx context.Context, v T, revision string) (string, error) {
	b, err := Encode(o.ops.Diff(o.base, v))
	if err != nil {
		return "", err
	}
	rev, err := o.doc.Save(ctx, b, revision)
	if err == nil {
		o.refresh()
	}
	return rev, err
}

// Encode is a document as stored: indented JSON with a final newline.
func Encode(v any) ([]byte, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Same reports whether a and b are stored alike: their JSON is equal.
func Same(a, b any) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}
