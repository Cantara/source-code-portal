package authz_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/cantara/source-code-portal/internal/freshness"
	"github.com/cantara/source-code-portal/internal/httpapi/authz"
	"github.com/cantara/source-code-portal/internal/readmodel"
	"github.com/cantara/source-code-portal/internal/scm"
)

type allowAll struct{}

func (allowAll) CanView(context.Context, authz.Principal, scm.Visibility) bool { return true }

type onlySubject string

func (o onlySubject) CanView(_ context.Context, p authz.Principal, v scm.Visibility) bool {
	return v == scm.VisPublic || p.Subject == string(o)
}

func TestLookupRepo(t *testing.T) {
	slot := readmodel.NewSlotForTest[freshness.Value[scm.Repository]]("catalog.repo")
	mk := func(v scm.Visibility) freshness.Value[scm.Repository] {
		return freshness.Value[scm.Repository]{V: scm.Repository{Slug: "x", Visibility: v}}
	}
	s := readmodel.NewSnapshotForTest(map[string]map[readmodel.EntryID]any{"catalog.repo": {"pub": mk(scm.VisPublic), "priv": mk(scm.VisPrivate)}})
	anon := context.Background()
	user := authz.WithPrincipal(anon, authz.NewPrincipal("alice", nil))

	if _, p := authz.LookupRepo(anon, s, slot, authz.PublicOnly{}, "pub"); p != nil {
		t.Fatal(p)
	}
	_, missing := authz.LookupRepo(anon, s, slot, authz.PublicOnly{}, "nope")
	_, private := authz.LookupRepo(anon, s, slot, authz.PublicOnly{}, "priv")
	_, bad := authz.LookupRepo(anon, s, slot, authz.PublicOnly{}, "..")
	_, reserved := authz.LookupRepo(anon, s, slot, authz.PublicOnly{}, "ext")
	_, upper := authz.LookupRepo(anon, s, slot, authz.PublicOnly{}, "Pub")
	if missing == nil || !reflect.DeepEqual(missing, private) || !reflect.DeepEqual(missing, bad) ||
		!reflect.DeepEqual(missing, reserved) || !reflect.DeepEqual(missing, upper) || missing.Status != 404 {
		t.Fatalf("existence oracle: %+v vs %+v", missing, private)
	}
	if _, p := authz.LookupRepo(user, s, slot, onlySubject("alice"), "priv"); p != nil {
		t.Fatal("alice should see priv")
	}
	if _, p := authz.LookupRepo(user, s, slot, onlySubject("bob"), "priv"); p == nil {
		t.Fatal("alice is not bob")
	}
}

func TestClassOf(t *testing.T) {
	if authz.ClassOf(authz.PublicOnly{}, authz.Principal{}) != scm.ViewPublic {
		t.Fatal("public only")
	}
	if authz.ClassOf(allowAll{}, authz.NewPrincipal("a", nil)) != scm.ViewAll {
		t.Fatal("all")
	}
	// An authorizer that denies VisPublic still cannot hide public data.
	if !authz.NewViewer(context.Background(), denyAll{}, authz.Principal{}).Can(scm.VisPublic) {
		t.Fatal("public is always visible")
	}
	if (authz.Principal{Subject: ""}).IsAnonymous() != true || authz.PrincipalFrom(context.Background()).IsAnonymous() != true {
		t.Fatal("zero principal must be anonymous")
	}
}

type denyAll struct{}

func (denyAll) CanView(context.Context, authz.Principal, scm.Visibility) bool { return false }
