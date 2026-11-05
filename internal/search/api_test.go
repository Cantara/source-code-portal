package search

import (
	"context"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/httpapi/oapi"
	"github.com/cantara/source-code-portal/internal/snapshot"
)

func TestAPI(t *testing.T) {
	full := catalogue(t).Current()
	var ops []string
	api := func(s *snapshot.Snapshot) API {
		return API{Current: func(context.Context) *snapshot.Snapshot { return s },
			Observe: func(op string, _ time.Duration) { ops = append(ops, op) }}
	}
	kind := oapi.SearchKindDoc
	res, _ := api(full).Search(t.Context(), oapi.SearchRequestObject{Params: oapi.SearchParams{Q: "sesion", In: &kind}})
	ok, isOK := res.(oapi.Search200JSONResponse)
	if !isOK {
		t.Fatalf("search: %#v", res)
	}
	b := ok.Body
	if b.Query != "sesion" || b.Counts.Doc != 1 || len(b.Hits) != 1 || b.Hits[0].Kind != oapi.SearchKindDoc || b.Hits[0].Url != "/repos/acme/whydah-core#configure" ||
		*b.Hits[0].Repo != "acme/whydah-core" || b.Hits[0].At != nil || b.Page != (oapi.Page{Number: 1, Size: 25, Total: 1, Pages: 1}) ||
		len(b.Corrections) != 1 || b.Corrections[0].To != "session" {
		t.Errorf("search body: %+v", b)
	}
	if marked := b.Hits[0].Snippet[1]; marked.Mark == nil || !*marked.Mark || marked.Text != "session" {
		t.Errorf("snippet: %+v", b.Hits[0].Snippet)
	}
	limit, page := 1, 2
	res, _ = api(full).Search(t.Context(), oapi.SearchRequestObject{Params: oapi.SearchParams{Q: "oauth", Limit: &limit, Page: &page}})
	if ok := res.(oapi.Search200JSONResponse).Body; ok.Page != (oapi.Page{Number: 2, Size: 1, Total: 2, Pages: 2}) || len(ok.Hits) != 1 {
		t.Errorf("page 2: %+v", ok)
	}
	big := 2000
	for _, p := range []oapi.SearchParams{{Q: ""}, {Q: "in:robots x"}, {Q: "oauth", Page: &big}, {Q: "oauth", Limit: &big}} {
		res, _ := api(full).Search(t.Context(), oapi.SearchRequestObject{Params: p})
		if _, bad := res.(oapi.Search400ApplicationProblemPlusJSONResponse); !bad {
			t.Errorf("%+v: %#v, want 400", p, res)
		}
	}
	sres, _ := api(full.Public()).Suggest(t.Context(), oapi.SuggestRequestObject{Params: oapi.SuggestParams{Q: "wh"}})
	if hits := sres.(oapi.Suggest200JSONResponse).Body.Hits; len(hits) != 1 || hits[0].Url != "/repos/acme/whydah-core" || hits[0].Snippet == nil {
		t.Errorf("suggest: %+v", hits)
	}
	if res, _ := api(&snapshot.Snapshot{}).Search(t.Context(), oapi.SearchRequestObject{Params: oapi.SearchParams{Q: "x"}}); len(res.(oapi.Search200JSONResponse).Body.Hits) != 0 {
		t.Error("an empty snapshot found something")
	}
	if len(ops) != 8 || ops[0] != "search" || ops[6] != "suggest" {
		t.Errorf("observed %q", ops)
	}
}
