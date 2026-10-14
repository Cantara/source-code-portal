package apitest

// Addition that design unit 07 asks of unit 06's apitest (section 12.3): a zero time must never reach a body.
//
// The contract's Timestamp pattern accepts "0001-01-01T00:00:00Z" (Go's zero time.Time) and "1970-01-01T00:00:00Z" (the Unix
// epoch), so response validation passes a handler that forgot to map a missing time to null. A never-seen source must be null,
// never "1970-01-01" (GR-29, GR-38). ZeroTimes finds them; NoZeroTimes fails the test.

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sort"
	"testing"
)

var zeroTimes = map[string]bool{"0001-01-01T00:00:00Z": true, "1970-01-01T00:00:00Z": true}

// ZeroTimes returns the JSON pointers of every string in a JSON document that is a zero time. A body that is not JSON has none.
func ZeroTimes(body []byte) []string {
	var v any
	if json.Unmarshal(body, &v) != nil {
		return nil
	}
	var out []string
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case string:
			if zeroTimes[x] {
				out = append(out, path)
			}
		case []any:
			for i, e := range x {
				walk(fmt.Sprintf("%s/%d", path, i), e)
			}
		case map[string]any:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(path+"/"+k, x[k])
			}
		}
	}
	walk("", v)
	return out
}

// NoZeroTimes fails the test when the recorded body contains a zero time. ValidateResponse of unit 06 calls it for every 2xx JSON body.
func NoZeroTimes(t testing.TB, rec *httptest.ResponseRecorder) {
	t.Helper()
	if z := ZeroTimes(rec.Body.Bytes()); len(z) > 0 {
		t.Errorf("zero time in the response body at %v: map a missing time to null (the contract's Timestamp pattern accepts it)", z)
	}
}
