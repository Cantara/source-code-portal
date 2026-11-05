package search

import (
	"regexp"
	"strings"
)

// Anchor is a heading of a rendered document with the id its page gives it.
type Anchor struct {
	Text string
	ID   string
}

// Section is the text of a Markdown document between two headings: the start before the first heading has none.
type Section struct {
	Heading string
	Anchor  string // the heading's id on the page; "" when the page has none for it
	Text    string // plain text: links as their text, images as their alt text, no HTML tags or front matter
}

var (
	atx      = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.*?)(?:[ \t]+#+)?[ \t]*$`)
	fence    = regexp.MustCompile("^ {0,3}(```|~~~)")
	image    = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	link     = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	refLink  = regexp.MustCompile(`\[([^\]]*)\]\[[^\]]*\]`)
	tag      = regexp.MustCompile(`<[^>]{0,512}>`)
	emphasis = regexp.MustCompile("[*`]+")
)

// Sections cuts a Markdown document at its headings of levels 1 to 3 (ATX headings outside code fences), and names each section's
// anchor from anchors, the page's headings in order, matched by their text.
func Sections(markdown string, anchors []Anchor) []Section {
	markdown = withoutFrontMatter(markdown)
	var out []Section
	cur := Section{}
	var text strings.Builder
	flush := func() {
		cur.Text = strings.TrimSpace(text.String())
		if cur.Text != "" || cur.Heading != "" {
			out = append(out, cur)
		}
		text.Reset()
	}
	next := 0
	inFence := ""
	for line := range strings.Lines(markdown) {
		line = strings.TrimRight(line, "\r\n")
		if m := fence.FindStringSubmatch(line); m != nil {
			switch inFence {
			case "":
				inFence = m[1]
			case m[1]:
				inFence = ""
			}
			continue
		}
		if inFence == "" {
			if m := atx.FindStringSubmatch(line); m != nil && len(m[1]) <= 3 {
				flush()
				heading := plain(m[2])
				cur = Section{Heading: heading}
				for i := next; i < len(anchors); i++ {
					if strings.EqualFold(strings.TrimSpace(anchors[i].Text), heading) {
						cur.Anchor, next = anchors[i].ID, i+1
						break
					}
				}
				continue
			}
		}
		text.WriteString(plain(line))
		text.WriteByte('\n')
	}
	flush()
	return out
}

// plain is a line of Markdown as text.
func plain(s string) string {
	s = image.ReplaceAllString(s, "$1")
	s = link.ReplaceAllString(s, "$1")
	s = refLink.ReplaceAllString(s, "$1")
	s = tag.ReplaceAllString(s, " ")
	return strings.TrimSpace(emphasis.ReplaceAllString(s, ""))
}

// withoutFrontMatter drops a leading YAML front matter block.
func withoutFrontMatter(s string) string {
	if !strings.HasPrefix(s, "---\n") && !strings.HasPrefix(s, "---\r\n") {
		return s
	}
	rest := s[strings.IndexByte(s, '\n')+1:]
	for i := 0; i < len(rest); {
		j := strings.IndexByte(rest[i:], '\n')
		line := rest[i:]
		if j >= 0 {
			line = rest[i : i+j]
		}
		if t := strings.TrimRight(line, "\r"); t == "---" || t == "..." {
			if j < 0 {
				return ""
			}
			return rest[i+j+1:]
		}
		if j < 0 {
			break
		}
		i += j + 1
	}
	return s
}
