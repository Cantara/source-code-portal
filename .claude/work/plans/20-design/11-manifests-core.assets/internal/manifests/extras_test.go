package manifests

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

func TestWarmItemRoundTrip(t *testing.T) {
	e := newEnv(t, map[string]string{"core": "core", "app": "app:core"})
	ctx := context.Background()
	if _, err := e.svc.runRefresh(ctx, rkey("app")); err != nil {
		t.Fatal(err)
	}
	want := e.facts(t, "app")
	it := e.st.items[store.ItemKey{Repo: store.RepoKey{Source: "github", Owner: "acme", Name: "app"}, Kind: ItemKind}]
	got, err := e.svc.WarmItem(it)
	if err != nil {
		t.Fatal(err)
	}
	if got.V.TreeSHA != want.V.TreeSHA || got.Info.State != "ok" || !reflect.DeepEqual(got.V.Units, want.V.Units) {
		t.Fatalf("warm value differs:\n%+v\n%+v", got.V, want.V)
	}
	it.Payload = []byte(`{"schema":99}`)
	if _, err := e.svc.WarmItem(it); !errors.Is(err, ErrSchema) {
		t.Fatalf("err=%v", err)
	}
	it.Key.Kind = "other"
	if _, err := e.svc.WarmItem(it); err == nil {
		t.Fatal("wrong kind accepted")
	}
}

func TestBadgeView(t *testing.T) {
	_, mux := apiEnv(t)
	_ = mux
	e, _ := apiEnv(t)
	b, ok := e.svc.BadgeView(e.pub.Current(), "app", 0)
	if !ok || b.InternalDeps != 1 || b.Stale || !b.Recognised || b.Info.State != freshness.StateOK {
		t.Fatalf("%+v", b)
	}
	if _, ok := e.svc.BadgeView(e.pub.Current(), "ghost", 0); ok {
		t.Fatal("unknown repo")
	}
	_ = readmodel.EntryID("")
}

func TestPrivacyGuard(t *testing.T) {
	internal := ComponentID{Maven, "org.acme", "core"}
	g := NewPrivacyGuard(PomSourceConfig{PrivateGroupPrefixes: []string{"com.secret", " "}}, func(id ComponentID) bool { return id == internal })
	for id, want := range map[ComponentID]bool{
		{Maven, "org.apache", "log4j"}: true,
		{Maven, "com.secret", "x"}:     false,
		{Maven, "com.secret.sub", "x"}: false,
		{Maven, "com.secretive", "x"}:  true,
		{Maven, "COM.Secret", "x"}:     false,
		internal:                       false,
	} {
		if g.Allow(id) != want {
			t.Errorf("%v: allow=%v want %v", id, !want, want)
		}
	}
	if !NewPrivacyGuard(PomSourceConfig{}, nil).Allow(internal) {
		t.Fatal("nil internal func means nothing is internal")
	}
}

func TestTechnologyAndHealth(t *testing.T) {
	e, _ := apiEnv(t)
	snap := e.pub.Current()
	v, ok := e.svc.Technology(snap, "app", scm.ViewPublic)
	if !ok || v.Units != 1 || v.DirectDependencies != 2 || v.InternalDependencies != 1 || v.Info.State != freshness.StateOK || len(v.Languages) != 1 ||
		v.Languages[0].Name != "JavaScript" || v.Languages[0].Share != 1 || len(v.Ecosystems) != 1 || v.Ecosystems[0] != "npm" || v.ParsedAt.IsZero() {
		t.Fatalf("%+v", v)
	}
	if _, ok := e.svc.Technology(snap, "ghost", scm.ViewPublic); ok {
		t.Fatal("unknown repository")
	}
	h := e.svc.Health(snap, time.Now())
	if !h.Enabled || h.StaleRepos != 0 || h.DanglingUnits != 0 || len(h.DisabledParsers) != 0 || h.RepositoriesWithDiagnostics != 0 {
		t.Fatalf("%+v", h)
	}
	if h := e.svc.Health(snap, time.Now().Add(48*time.Hour)); h.StaleRepos != 3 {
		t.Fatalf("stale after two days: %+v", h)
	}
	if !e.svc.Enabled() {
		t.Fatal("enabled")
	}
	if dep, used := e.svc.Related(snap, "app", scm.ViewPublic); !reflect.DeepEqual(dep, []scm.Slug{"core"}) || len(used) != 0 {
		t.Fatalf("related %v %v", dep, used)
	}
	if dep, used := e.svc.Related(snap, "core", scm.ViewPublic); len(dep) != 0 || !reflect.DeepEqual(used, []scm.Slug{"app"}) {
		t.Fatalf("related %v %v", dep, used)
	}
}
