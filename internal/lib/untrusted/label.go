package untrusted

import "strings"

// MaxLabel is the longest label name (or issue type) kept.
const MaxLabel = 50

// Label is a label of a pull request or an issue, as its repository's text gave it: cleaned and bounded.
type Label struct {
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// NewLabel is a label cleaned: its name as one bounded line, its colour as six lower-case hex digits or none.
func NewLabel(name, color string) Label {
	return Label{Name: Text(name, MaxLabel), Color: hexColor(color)}
}

// hexColor is a label colour as six hex digits, or empty.
func hexColor(s string) string {
	if len(s) != 6 {
		return ""
	}
	s = strings.ToLower(s)
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ""
		}
	}
	return s
}
