package san

import "testing"

func FuzzSanitize(f *testing.F) {
	for _, s := range []string{"<img src=x onerror=alert(1)>", "[a](javascript:x)", "<svg><a xlink:href=javascript:1>", "<math><mi><style><img src onerror=1>"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		a, err := Render([]byte(in))
		if err != nil {
			return
		}
		// fixpoint: sanitising already-sanitised output must not change it (mXSS canary)
		if b := policy.Sanitize(a.String()); b != a.String() {
			t.Fatalf("not idempotent:\n%q\n%q", a.String(), b)
		}
	})
}
