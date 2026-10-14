package commits

import (
	"strings"
	"testing"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
)

func TestNormalise(t *testing.T) {
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	c := func(sha string, at time.Time, subj string) scm.Commit {
		return scm.Commit{SHA: sha, CommittedAt: at, Subject: subj, URL: "https://github.com/o/r/commit/" + sha}
	}
	in := []scm.Commit{
		c(sha(1), now.Add(-time.Hour), "first\nsecond line"),                // tip, multi-line subject
		c(sha(2), now.Add(-2*time.Hour), "dup"),                             // kept
		c(sha(2), now.Add(-3*time.Hour), "dup again"),                       // duplicate
		c("xyz", now, "bad sha"),                                            // invalid sha
		c(sha(3), time.Time{}, "no date"),                                   // zero date
		c(sha(4), now.Add(90*24*time.Hour), "from the future"),              // clamped
		c(strings.ToUpper(sha(5)), now.Add(-5*time.Hour), "upper case sha"), // lower-cased
	}
	out, head, st := normalise(in, now, 30, "https://github.com/o/r", nil)
	if head != sha(1) {
		t.Fatalf("head %q", head)
	}
	if st != (dropStats{NoDate: 1, InvalidSHA: 1, Duplicate: 1, Clamped: 1}) {
		t.Fatalf("stats %+v", st)
	}
	var got []string
	for _, x := range out {
		got = append(got, x.SHA[39:]+"@"+x.CommittedAt.Format("15:04"))
	}
	want := []string{"4@10:00", "1@09:00", "2@08:00", "5@05:00"} // clamped commit sorts first at now
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order %v want %v", got, want)
	}
	if out[1].Subject != "first" {
		t.Fatalf("subject %q", out[1].Subject)
	}
}

func TestNormaliseDerivesURLFromRepository(t *testing.T) {
	in := []scm.Commit{{SHA: sha(1), CommittedAt: base, URL: "javascript:alert(1)"}, {SHA: sha(2), CommittedAt: base.Add(-time.Hour)}}
	out, _, st := normalise(in, base, 30, "https://github.com/o/r", nil)
	if len(out) != 2 || out[0].URL != "https://github.com/o/r/commit/"+sha(1) || st.NoURL != 0 {
		t.Fatalf("%+v %+v", out, st)
	}
	out, _, st = normalise(in, base, 30, "", nil)
	if len(out) != 0 || st.NoURL != 2 {
		t.Fatalf("no usable URL anywhere must drop the commit: %+v %+v", out, st)
	}
}

func TestNormaliseCapsAndKeepsNewest(t *testing.T) {
	now := base
	var in []scm.Commit
	for i := 0; i < 50; i++ {
		in = append(in, scm.Commit{SHA: sha(i + 1), CommittedAt: now.Add(-time.Duration(i) * time.Minute)})
	}
	out, _, _ := normalise(in, now, 30, "https://github.com/o/r", nil)
	if len(out) != 30 || out[0].SHA != sha(1) || out[29].SHA != sha(30) {
		t.Fatalf("len %d first %s last %s", len(out), out[0].SHA[39:], out[len(out)-1].SHA[39:])
	}
}

func TestCleanLine(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"  hello   world  ", "hello world"},
		{"line one\r\nline two", "line one"},
		{"a‮b​c", "abc"},                                                 // bidi override and zero-width space removed
		{"tab\there", "tab here"},                                        // control becomes a space
		{"\x00\x01nul", "nul"},                                           // leading controls vanish
		{"bad\xffutf8", "bad�utf8"},                                      // invalid UTF-8 becomes U+FFFD
		{"family \U0001F468‍\U0001F469", "family \U0001F468‍\U0001F469"}, // ZWJ kept
	} {
		if got := cleanLine(tc.in, 200); got != tc.want {
			t.Errorf("cleanLine(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	long := strings.Repeat("ø", 300)
	got := cleanLine(long, 200)
	if n := len([]rune(got)); n != 200 || !strings.HasSuffix(got, "…") {
		t.Errorf("cap: %d runes, suffix %q", n, got[len(got)-3:])
	}
	if got := cleanLine(strings.Repeat("a", 200), 200); len(got) != 200 {
		t.Errorf("exact fit truncated: %d", len(got))
	}
}

func TestCleanURLAndLogin(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/o/r/commit/abc": "https://github.com/o/r/commit/abc",
		"javascript:alert(1)":               "",
		"https://user:pw@github.com/x":      "",
		"//github.com/x":                    "",
		"":                                  "",
		"http://ghe.internal/o/r":           "",
	} {
		if got := cleanURL(in); got != want {
			t.Errorf("cleanURL(%q) = %q want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"octocat": "octocat", "dependabot[bot]": "dependabot[bot]", "-bad": "", "a b": "", "": "", strings.Repeat("a", 40): ""} {
		if got := cleanLogin(in); got != want {
			t.Errorf("cleanLogin(%q) = %q want %q", in, got, want)
		}
	}
}

// A commit clamped at one ingestion keeps its clamped date at the next, so it ages instead of tracking "now".
func TestNormaliseClampIsStableAcrossRefreshes(t *testing.T) {
	t0 := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	sha := strings.Repeat("a", 40)
	in := []scm.Commit{{SHA: sha, CommittedAt: time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC), URL: "https://github.com/o/r/commit/" + sha}}
	first, _, st := normalise(in, t0, 30, "", nil)
	if st.Clamped != 1 || !first[0].CommittedAt.Equal(t0) {
		t.Fatalf("first: %v %+v", first[0].CommittedAt, st)
	}
	t1 := t0.Add(3 * time.Hour)
	second, _, _ := normalise(in, t1, 30, "", first)
	if !second[0].CommittedAt.Equal(t0) {
		t.Fatalf("re-clamped to the new now: %v", second[0].CommittedAt)
	}
}
