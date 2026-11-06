package scope

import "testing"

func TestParams(t *testing.T) {
	ptr := func(n int) *int { return &n }
	for _, c := range []struct {
		page, limit *int
		number, siz int
		problem     string
	}{
		{nil, nil, 1, 50, ""},
		{ptr(3), ptr(200), 3, 200, ""},
		{ptr(0), nil, 0, 0, "page must be 1 to 100000"},
		{ptr(100001), nil, 0, 0, "page must be 1 to 100000"},
		{nil, ptr(0), 0, 0, "limit must be 1 to 200"},
		{nil, ptr(201), 0, 0, "limit must be 1 to 200"},
	} {
		number, size, problem := Params(c.page, c.limit, DefaultLimit)
		if number != c.number || size != c.siz || problem != c.problem {
			t.Errorf("Params(%v, %v) = %d, %d, %q; want %d, %d, %q", c.page, c.limit, number, size, problem, c.number, c.siz, c.problem)
		}
	}
}

func TestPaging(t *testing.T) {
	start, end, p := Paging(55, 2, 25)
	if start != 25 || end != 50 || p.Pages != 3 || p.Total != 55 {
		t.Errorf("page 2 of 55 by 25 = %d..%d, %+v", start, end, p)
	}
	start, end, p = Paging(55, 9, 25) // past the last page: empty
	if start != 55 || end != 55 || p.Pages != 3 {
		t.Errorf("past the end = %d..%d, %+v", start, end, p)
	}
	if _, _, p = Paging(0, 1, 25); p.Pages != 1 {
		t.Errorf("an empty list has one page, got %d", p.Pages)
	}
}
