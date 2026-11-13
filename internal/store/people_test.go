package store

import (
	"slices"
	"testing"
	"time"
)

// The enterprise mode's people survive a restart: a person, their token sealed as given and what it lists; a refused token keeps
// nothing listed; a replaced token starts with nothing listed; a removed one leaves nothing behind.
func TestPeopleSurviveARestart(t *testing.T) {
	db, path := openTemp(t)
	ctx := t.Context()
	at := time.Unix(1_800_000_000, 0)
	if err := db.PutPerson(ctx, Person{ID: "iss#ann", Identity: "ann@example.org", Name: "Ann", FirstAt: at, LastAt: at}); err != nil {
		t.Fatal(err)
	}
	if err := db.PutPerson(ctx, Person{ID: "iss#ann", Identity: "ann@example.org", Name: "Ann B", FirstAt: at.Add(time.Hour), LastAt: at.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	tok := PersonToken{Person: "iss#ann", Source: "ghe", Owner: "acme", Login: "ann_oc", Sealed: []byte{1, 2, 3}, ExpiresAt: at.Add(90 * 24 * time.Hour),
		CreatedAt: at, State: "ok"}
	if err := db.PutPersonToken(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if err := db.SetAccess(ctx, Access{Person: "iss#ann", Source: "ghe", Owner: "acme", Repos: []int64{7, 3, 7}}, at); err != nil {
		t.Fatal(err)
	}
	if err := db.SetAccess(ctx, Access{Person: "iss#bo", Source: "ghe", Owner: "acme", Repos: []int64{9}}, at); err == nil {
		t.Error("an access list without its token was kept")
	}
	_ = db.Close()

	db, err := OpenAs(ctx, path, Enterprise, quiet)
	if err != nil {
		t.Fatal(err)
	}
	people, tokens, access, err := db.People(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(people) != 1 || people[0].Name != "Ann B" || !people[0].FirstAt.Equal(at) || !people[0].LastAt.Equal(at.Add(time.Hour)) {
		t.Errorf("people %+v", people)
	}
	if len(tokens) != 1 || !slices.Equal(tokens[0].Sealed, tok.Sealed) || !tokens[0].ExpiresAt.Equal(tok.ExpiresAt) || !tokens[0].ListedAt.Equal(at) ||
		tokens[0].Login != "ann_oc" || tokens[0].State != "ok" {
		t.Errorf("tokens %+v", tokens)
	}
	if len(access) != 1 || !slices.Equal(access[0].Repos, []int64{3, 7}) {
		t.Errorf("access %+v", access)
	}

	if err := db.SetTokenState(ctx, "iss#ann", "ghe", "acme", "unauthorized"); err != nil {
		t.Fatal(err)
	}
	if _, tokens, access, _ := db.People(ctx); len(access) != 0 || tokens[0].State != "unauthorized" || !tokens[0].ListedAt.IsZero() {
		t.Errorf("after a refusal: tokens %+v, access %+v", tokens, access)
	}
	if err := db.SetAccess(ctx, Access{Person: "iss#ann", Source: "ghe", Owner: "acme", Repos: []int64{3}}, at); err != nil {
		t.Fatal(err)
	}
	if err := db.PutPersonToken(ctx, tok); err != nil {
		t.Fatal(err)
	}
	if _, _, access, _ := db.People(ctx); len(access) != 0 {
		t.Errorf("a replaced token kept its predecessor's list: %+v", access)
	}
	if err := db.DeletePersonToken(ctx, "iss#ann", "ghe", "acme"); err != nil {
		t.Fatal(err)
	}
	if _, tokens, access, _ := db.People(ctx); len(tokens) != 0 || len(access) != 0 {
		t.Errorf("after removal: %+v %+v", tokens, access)
	}
	_ = db.Close()
}
