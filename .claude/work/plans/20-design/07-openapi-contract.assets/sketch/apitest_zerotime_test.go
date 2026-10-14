package apitest

import (
	"reflect"
	"testing"
)

func TestZeroTimes(t *testing.T) {
	body := []byte(`{"fetchedAt":"0001-01-01T00:00:00Z","items":[{"staleSince":"1970-01-01T00:00:00Z"},{"asOf":"2026-10-14T09:41:07Z","t":null}],"n":"x"}`)
	want := []string{"/fetchedAt", "/items/0/staleSince"}
	if got := ZeroTimes(body); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := ZeroTimes([]byte(`{"a":"2026-10-14T09:41:07Z"}`)); len(got) != 0 {
		t.Fatalf("a real time is not a zero time: %v", got)
	}
	if got := ZeroTimes([]byte(`not json`)); got != nil {
		t.Fatalf("non-JSON has none: %v", got)
	}
}
