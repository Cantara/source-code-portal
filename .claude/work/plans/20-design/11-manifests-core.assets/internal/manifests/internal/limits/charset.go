package limits

import (
	"io"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

// CharsetReader is an encoding/xml Decoder.CharsetReader. It supports exactly the single-byte charsets that real
// pom.xml and *.csproj files declare (iso-8859-1, windows-1252, us-ascii, utf-8) and rejects every other declared
// charset with an EncodingError (Go's decoder fails without a CharsetReader). UTF-16 is rejected earlier by RejectBinary.
func CharsetReader(label string, input io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "utf-8", "utf8", "us-ascii", "ascii":
		return input, nil
	case "iso-8859-1", "iso8859-1", "latin1", "latin-1":
		return charmap.ISO8859_1.NewDecoder().Reader(input), nil
	case "windows-1252", "cp1252":
		return charmap.Windows1252.NewDecoder().Reader(input), nil
	}
	return nil, &EncodingError{Reason: "charset:" + strings.ToLower(label)}
}
