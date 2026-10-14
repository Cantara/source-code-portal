package media

import (
	"bytes"
	"encoding/xml"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"
)

// Kind is the verdict of Sniff.
type Kind string

const (
	KindPNG     Kind = "image/png"
	KindJPEG    Kind = "image/jpeg"
	KindGIF     Kind = "image/gif"
	KindWebP    Kind = "image/webp"
	KindAVIF    Kind = "image/avif"
	KindSVG     Kind = "image/svg+xml"
	KindUnknown Kind = ""
)

// Sniff decides the content type from the bytes, never from the upstream header or the file name. Only the raster
// types and SVG are recognised; everything else (HTML, text, PDF, video, ICO, BMP) is KindUnknown.
func Sniff(b []byte) Kind {
	head := b
	if len(head) > 512 {
		head = head[:512]
	}
	switch http.DetectContentType(head) {
	case "image/png":
		return KindPNG
	case "image/jpeg":
		return KindJPEG
	case "image/gif":
		return KindGIF
	case "image/webp":
		return KindWebP
	}
	if len(b) >= 12 && string(b[4:8]) == "ftyp" {
		switch string(b[8:12]) {
		case "avif", "avis":
			return KindAVIF
		}
	}
	if looksLikeSVG(b) {
		return KindSVG
	}
	return KindUnknown
}

// looksLikeSVG is true when the first start element of a well-formed prefix is <svg>. It scans at most 64 tokens, uses
// encoding/xml (no entity expansion beyond the five predefined ones, no external entities) and never resolves DTDs.
func looksLikeSVG(b []byte) bool {
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	if len(b) == 0 || !utf8.Valid(b[:min(len(b), 4096)]) {
		return false
	}
	if i := bytes.IndexByte(b, '<'); i < 0 || len(bytes.TrimSpace(b[:i])) != 0 {
		return false
	}
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = false
	d.Entity = map[string]string{}
	for i := 0; i < 64; i++ {
		t, err := d.RawToken()
		if err != nil && err != io.EOF {
			return false
		}
		if dir, ok := t.(xml.Directive); ok {
			f := strings.Fields(string(dir))
			if len(f) >= 2 && strings.EqualFold(f[0], "DOCTYPE") && f[1] != "svg" {
				return false // <!doctype html> and friends
			}
			if strings.ContainsAny(string(dir), "[") || strings.Contains(strings.ToUpper(string(dir)), "<!ENTITY") {
				return false // an internal DTD subset: entity expansion is the browser's, refuse it (V-04)
			}
		}
		if se, ok := t.(xml.StartElement); ok {
			return strings.EqualFold(se.Name.Local, "svg") && (se.Name.Space == "" || strings.EqualFold(se.Name.Space, "svg"))
		}
		if err == io.EOF {
			return false
		}
	}
	return false
}
