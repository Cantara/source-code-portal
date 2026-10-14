package integrations_test

// Regression tests added by the independent verification of 2026-10-14 (see 30-verification/design-14-integrations-access.md).

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/integrations"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

func twoInstances() integrations.Config {
	c := integrations.DefaultConfig()
	c.Jenkins.Instances = []integrations.JenkinsInstance{
		{Name: "main", Default: true, BaseURL: "https://jenkins.example.org"},
		{Name: "other", BaseURL: "https://ci.example.net"},
	}
	c.Jenkins.Concurrency, c.Jenkins.AbortAfter = 1, 2
	return c
}

// declaredOn puts every repository on an instance through the descriptor.
func declaredOn(inst map[string]string) (ovr, []scm.Repository) {
	o := ovr{}
	var rs []scm.Repository
	for slug, in := range inst {
		o[scm.Slug(slug)] = map[string]map[string]string{"jenkins": {"job": slug, "instance": in}}
		rs = append(rs, repo(strings.ToUpper(slug[:1])+slug[1:], 0))
	}
	return o, rs
}

// One dead instance among two must not park or back off the sweeps of the healthy one.
func TestOneAbortedInstanceDoesNotFailTheSweep(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ov, repos := declaredOn(map[string]string{"alpha": "main", "beta": "main", "gamma": "other", "delta": "other", "eps": "other"})
		e := newEnv(t, repos, nil, nil)
		fp := &fakeProvider{name: "jenkins", fn: func(tg integrations.Target) (integrations.Status, error) {
			if tg.Instance == "other" {
				return integrations.Status{}, &integrations.PermanentError{Provider: "jenkins", Code: "auth", Err: errors.New("401")}
			}
			return integrations.Status{State: integrations.StateOK, Summary: "build #1 passed"}, nil
		}}
		s := svc(t, e, twoInstances(), newMemStore(), ov, fp)
		if _, err := s.Sweep(context.Background(), "jenkins"); err != nil {
			t.Fatalf("a permanent error of one instance must not be a sweep error while another is healthy: %v", err)
		}
		if v := s.StatusOf(e.pub.Current(), "alpha", "jenkins"); v.V.State != integrations.StateOK {
			t.Fatalf("alpha = %+v", v)
		}
		if h := s.Health()[0]; h.LastError != "auth" || h.ConsecutiveFailures != 1 || !h.LastSuccess.IsZero() {
			t.Fatalf("health must report the aborted instance: %+v", h)
		}
	})
}

// When every instance is aborted the permanent cause wins over a transient one.
func TestAllInstancesAbortedPrefersPermanent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ov, repos := declaredOn(map[string]string{"alpha": "main", "beta": "main", "gamma": "other", "delta": "other"})
		e := newEnv(t, repos, nil, nil)
		fp := &fakeProvider{name: "jenkins", fn: func(tg integrations.Target) (integrations.Status, error) {
			if tg.Instance == "other" {
				return integrations.Status{}, &integrations.PermanentError{Provider: "jenkins", Code: "auth", Err: errors.New("401")}
			}
			return integrations.Status{}, errors.New("i/o timeout")
		}}
		s := svc(t, e, twoInstances(), newMemStore(), ov, fp)
		_, err := s.Sweep(context.Background(), "jenkins")
		if integrations.ErrCode(err) != "auth" {
			t.Fatalf("err = %v", err)
		}
	})
}

// 403 is per job: a few of them do not abort the instance; abort_after in a row do (anonymous read disabled).
func TestForbiddenIsPerTargetUntilARun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ov, repos := declared("alpha", "beta", "gamma", "delta", "eps", "zeta")
		e := newEnv(t, repos, nil, nil)
		cfg := jcfg()
		cfg.Jenkins.Concurrency, cfg.Jenkins.AbortAfter = 1, 3
		fp := &fakeProvider{name: "jenkins", fn: func(tg integrations.Target) (integrations.Status, error) {
			if tg.Repo.Name == "Beta" || tg.Repo.Name == "Delta" { // isolated forbidden jobs
				return integrations.Status{}, fmt.Errorf("HTTP 403: %w", scm.ErrForbidden)
			}
			return integrations.Status{State: integrations.StateOK, Summary: "build #1 passed"}, nil
		}}
		s := svc(t, e, cfg, newMemStore(), ov, fp)
		if _, err := s.Sweep(context.Background(), "jenkins"); err != nil {
			t.Fatalf("isolated 403s must not fail the sweep: %v", err)
		}
		if fp.n != 6 {
			t.Fatalf("calls = %d, the instance must not be aborted by non-consecutive 403s", fp.n)
		}
		if v := s.StatusOf(e.pub.Current(), "beta", "jenkins"); v.Info.ErrCode != "forbidden" {
			t.Fatalf("beta = %+v", v)
		}
		// everything forbidden: abort after 3 in a row, permanent auth
		fp2 := &fakeProvider{name: "jenkins", fn: func(integrations.Target) (integrations.Status, error) {
			return integrations.Status{}, fmt.Errorf("HTTP 403: %w", scm.ErrForbidden)
		}}
		e2 := newEnv(t, repos, nil, nil)
		s2 := svc(t, e2, cfg, newMemStore(), ov, fp2)
		_, err := s2.Sweep(context.Background(), "jenkins")
		var p interface{ Permanent() bool }
		if !errors.As(err, &p) || integrations.ErrCode(err) != "auth" || fp2.n != 3 {
			t.Fatalf("err = %v calls = %d", err, fp2.n)
		}
	})
}

// A sweep in which every probed target failed (HTML login page, wrong base_url) is not a healthy sweep (GR-41).
func TestAllTargetsFailedIsNotASuccess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ov, repos := declared("alpha", "beta")
		e := newEnv(t, repos, nil, nil)
		fp := &fakeProvider{name: "jenkins", fn: func(integrations.Target) (integrations.Status, error) {
			return integrations.Status{}, fmt.Errorf("content type text/html: %w", integrations.ErrProtocol)
		}}
		s := svc(t, e, jcfg(), newMemStore(), ov, fp)
		for i := 1; i <= 3; i++ {
			if _, err := s.Sweep(context.Background(), "jenkins"); err != nil {
				t.Fatal(err)
			}
			time.Sleep(5 * time.Minute)
			h := s.Health()[0]
			if h.LastError != "protocol" || h.Failed != 2 || h.Probed != 2 || h.ConsecutiveFailures != i || !h.LastSuccess.IsZero() {
				t.Fatalf("sweep %d health = %+v", i, h)
			}
		}
		fp.fn = okStatus("")
		s.Sweep(context.Background(), "jenkins")
		if h := s.Health()[0]; h.LastError != "" || h.ConsecutiveFailures != 0 || h.LastSuccess.IsZero() {
			t.Fatalf("recovery health = %+v", h)
		}
	})
}

// A corrected descriptor is probed at once, not after not_found_ttl.
func TestCorrectedJobIsProbedDespiteNotFoundCarry(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ov, repos := declared("alpha")
		e := newEnv(t, repos, nil, nil)
		fp := &fakeProvider{name: "jenkins", fn: func(tg integrations.Target) (integrations.Status, error) {
			if tg.Job == "alpha" {
				return integrations.Status{}, fmt.Errorf("job not found: %w", scm.ErrNotFound)
			}
			return integrations.Status{State: integrations.StateOK, Summary: "build #7 passed"}, nil
		}}
		s := svc(t, e, jcfg(), newMemStore(), ov, fp)
		s.Sweep(context.Background(), "jenkins")
		time.Sleep(5 * time.Minute)
		ov["alpha"]["jenkins"]["job"] = "team/alpha"
		s.Sweep(context.Background(), "jenkins")
		if v := s.StatusOf(e.pub.Current(), "alpha", "jenkins"); v.V.State != integrations.StateOK || fp.n != 2 {
			t.Fatalf("v = %+v calls = %d", v, fp.n)
		}
	})
}

// Stored bytes are untrusted: a tampered row cannot put a javascript: link or arbitrary count keys into the slot.
func TestWarmItemRevalidatesPayload(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ov, repos := declared("alpha")
		e := newEnv(t, repos, nil, nil)
		s := svc(t, e, jcfg(), newMemStore(), ov, &fakeProvider{name: "jenkins", fn: okStatus("")})
		it := store.Item{Key: store.ItemKey{Repo: store.KeyOf(repos[0].Ref), Kind: "integrations.jenkins"}, State: freshness.StateOK, FetchedAt: time.Now(), CheckedAt: time.Now(),
			Payload: []byte(`{"schema":1,"state":"disabled","summary":"x","detailUrl":"javascript:alert(1)","counts":{"high":-4,"evil":9,"critical":99999999999}}`)}
		if _, err := e.pub.Apply(func(b *readmodel.Builder) error { return s.WarmItem(b, "alpha", it) }); err != nil {
			t.Fatal(err)
		}
		v := s.StatusOf(e.pub.Current(), "alpha", "jenkins")
		if v.V.DetailURL != "" || v.V.State != integrations.StateUnknown || v.V.Counts["evil"] != 0 || v.V.Counts["high"] != 0 || v.V.Counts["critical"] != integrations.MaxCount {
			t.Fatalf("v = %+v", v.V)
		}
	})
}

func TestConfigDefaultsAreAppliedByValidateAndNew(t *testing.T) {
	c := integrations.DefaultConfig()
	c.Jenkins.Instances = []integrations.JenkinsInstance{{Name: "main", BaseURL: "https://jenkins.example.org"}} // no ApplyInstanceDefaults
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate must default per-instance values itself: %v", err)
	}
	if c.Jenkins.Instances[0].Discover != "" {
		t.Fatal("the caller's slice was mutated")
	}
	e := newEnv(t, nil, nil, nil)
	if _, err := integrations.New(c, integrations.Deps{Publisher: e.pub, Status: e.status, Providers: []integrations.StatusProvider{&fakeProvider{name: "jenkins"}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := integrations.New(integrations.DefaultConfig(), integrations.Deps{}); err == nil {
		t.Fatal("a nil Publisher must be refused at construction")
	}
	c.Snyk.Enabled, c.Snyk.Ports = true, []int{0}
	if err := c.Validate(); err == nil || !strings.Contains(err.Error(), "snyk.ports") {
		t.Fatalf("snyk ports not validated: %v", err)
	}
}

// Unit 09's bridge maps tones by these exact names; an unknown name silently becomes neutral there.
func TestToneNamesAreTheCrossUnitContract(t *testing.T) {
	want := map[integrations.Tone]string{integrations.ToneGood: "success", integrations.ToneWarn: "warning", integrations.ToneBad: "danger", integrations.ToneNeutral: "neutral"}
	for tone, name := range want {
		if tone.String() != name {
			t.Errorf("%d = %q, want %q", tone, tone.String(), name)
		}
	}
}
