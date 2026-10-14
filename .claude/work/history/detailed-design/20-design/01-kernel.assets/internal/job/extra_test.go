package job_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/cantara/source-code-portal/internal/job"
)

func TestStringsAndParts(t *testing.T) {
	for c, w := range map[job.Class]string{job.ClassNone: "none", job.ClassTransient: "transient", job.ClassRateLimited: "rate_limited", job.ClassPermanent: "permanent", job.ClassGone: "gone", job.ClassCanceled: "canceled"} {
		if c.String() != w {
			t.Errorf("%d: %s", c, c.String())
		}
	}
	for p, w := range map[job.Priority]string{job.PrioInteractive: "interactive", job.PrioWebhook: "webhook", job.PrioScheduled: "scheduled", job.PrioBackfill: "backfill", 0: "invalid", 9: "invalid"} {
		if p.String() != w {
			t.Errorf("%d: %s", p, p.String())
		}
	}
	if job.Priority(0).Valid() || job.Priority(5).Valid() || job.Scope(0).Valid() || job.Scope(3).Valid() {
		t.Error("zero and out-of-range values are invalid")
	}
	k := job.NewKey("a.b", "x")
	if k.String() != "a.b/x" || len(k.Parts()) != 1 {
		t.Error(k)
	}
	if got := job.Key("nokind").Parts(); got != nil {
		t.Errorf("a key without a tail has no parts: %v", got)
	}
	base := errors.New("boom")
	if g := job.MarkGone(base); g.Error() != "boom" || !errors.Is(g, base) {
		t.Error("MarkGone keeps text and chain")
	}
	if p := job.MarkPermanent(base); p.Error() != "boom" || !errors.Is(p, base) {
		t.Error("MarkPermanent keeps text and chain")
	}
}

func TestClassStringIsTotal(t *testing.T) {
	if job.Class(200).String() != "invalid" {
		t.Error("an out-of-range class must not panic")
	}
}

func TestKeyPartsMustBeValidUTF8WithoutLineSeparators(t *testing.T) {
	for name, tail := range map[string]string{
		"invalid utf-8":       "a/\xff\xfe/c",
		"line separator":      "a/b\u2028x/c",
		"paragraph separator": "a/b\u2029x/c",
		"next line (C1)":      "a/b\u0085x/c",
		"tab":                 "a/b\tx/c",
	} {
		if _, err := job.ParseKey("commits.refresh/" + tail); err == nil {
			t.Errorf("%s: accepted", name)
		}
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: NewKey must panic", name)
				}
			}()
			job.NewKey("commits.refresh", strings.Split(tail, "/")...)
		}()
	}
	if _, err := job.ParseKey("commits.refresh/acme/ü/n"); err != nil {
		t.Errorf("a printable non-ASCII part is allowed at this layer: %v", err)
	}
}
