package manifests

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unique"
)

// Persisted payload schema versions. A reader that meets a larger Schema ignores the row (the job recomputes it).
const (
	FactsSchema = 1
	ParseSchema = 1
)

func intern(s string) string {
	if s == "" {
		return ""
	}
	return unique.Make(s).Value()
}

// wireDep is the persisted/flat JSON form of Dependency (the API DTO is a separate generated type, GR-11).
type wireDep struct {
	Eco   EcosystemID `json:"e"`
	Ns    string      `json:"ns,omitempty"`
	Name  string      `json:"n"`
	Disp  string      `json:"disp,omitempty"`
	Req   string      `json:"req,omitempty"`
	Ver   string      `json:"v,omitempty"`
	Res   Resolution  `json:"res,omitempty"`
	Scope Scope       `json:"sc,omitempty"`
	Group string      `json:"g,omitempty"`
	Opt   bool        `json:"opt,omitempty"`
	Cond  string      `json:"cond,omitempty"`
	Qual  string      `json:"q,omitempty"`
	Src   SourceKind  `json:"src,omitempty"`
	Ref   string      `json:"ref,omitempty"`
	Conf  Confidence  `json:"conf,omitempty"`
	Path  string      `json:"lp,omitempty"`
	Line  int32       `json:"l,omitempty"`
}

func (d Dependency) MarshalJSON() ([]byte, error) {
	return json.Marshal(wireDep{d.Target.Ecosystem, d.Target.Namespace, d.Target.Name, d.TargetDisplay(), d.Requirement, d.Version, d.Resolution,
		d.Scope, d.Group, d.Optional, d.Condition(), d.Qualifiers(), d.Source, d.SourceRef(), d.Confidence, d.Loc().Path, d.Line})
}

func (d *Dependency) UnmarshalJSON(b []byte) error {
	var w wireDep
	if err := json.Unmarshal(b, &w); err != nil {
		return err
	}
	*d = Dependency{Target: ComponentID{w.Eco, intern(w.Ns), intern(w.Name)}, Requirement: intern(w.Req), Version: intern(w.Ver), Group: intern(w.Group),
		Line: w.Line, Resolution: w.Res, Scope: w.Scope, Source: w.Src, Confidence: w.Conf, Optional: w.Opt}
	if w.Disp != "" || w.Cond != "" || w.Qual != "" || w.Ref != "" || w.Path != "" {
		d.Extra = &DependencyExtra{w.Disp, w.Cond, w.Qual, w.Ref, w.Path}
	}
	return nil
}

type factsRecord struct {
	Schema int       `json:"schema"`
	Facts  RepoFacts `json:"facts"`
}

// EncodeFacts produces the item payload (JSON, units without locked components). It fails when the result exceeds max bytes.
func EncodeFacts(f RepoFacts, max int) ([]byte, error) {
	f.Units = stripLocked(f.Units)
	b, err := json.Marshal(factsRecord{FactsSchema, f})
	if err != nil {
		return nil, err
	}
	if max > 0 && len(b) > max {
		return nil, fmt.Errorf("%w: %d bytes (max %d)", ErrFactsTooLarge, len(b), max)
	}
	return b, nil
}

var (
	ErrFactsTooLarge = errors.New("manifests: facts payload too large")
	ErrSchema        = errors.New("manifests: payload schema is newer than this build")
)

// DecodeFacts reads an item payload. Unknown fields are ignored (forward compatible); a newer schema returns ErrSchema.
func DecodeFacts(b []byte) (RepoFacts, error) {
	var r factsRecord
	if err := json.NewDecoder(bytes.NewReader(b)).Decode(&r); err != nil {
		return RepoFacts{}, err
	}
	if r.Schema > FactsSchema {
		return RepoFacts{}, ErrSchema
	}
	for i := range r.Facts.Units {
		r.Facts.Units[i].Dependencies = cloneExact(r.Facts.Units[i].Dependencies)
	}
	return r.Facts, nil
}

func cloneExact[T any](s []T) []T {
	if s == nil {
		return nil
	}
	out := make([]T, len(s))
	copy(out, s)
	return out
}

func stripLocked(us []Unit) []Unit {
	out := make([]Unit, len(us))
	for i, u := range us {
		if len(u.Locked) > 0 {
			u.LockedCount = len(u.Locked)
		}
		u.Locked = nil
		out[i] = u
	}
	return out
}

// parseRecord is the value of the manifests.parse cache: a pure function of (blob bytes, parser id, parser version).
type parseRecord struct {
	Schema int       `json:"schema"`
	Parsed Parsed    `json:"parsed"`
	Err    *parseErr `json:"err,omitempty"` // negative cache: the parse failed deterministically
}

type parseErr struct {
	Code string `json:"code"`
	Msg  string `json:"msg,omitempty"`
}

func EncodeParse(p Parsed, e *parseErr) ([]byte, error) {
	return json.Marshal(parseRecord{ParseSchema, p, e})
}

func DecodeParse(b []byte) (Parsed, *parseErr, error) {
	var r parseRecord
	if err := json.Unmarshal(b, &r); err != nil {
		return Parsed{}, nil, err
	}
	if r.Schema > ParseSchema {
		return Parsed{}, nil, ErrSchema
	}
	return r.Parsed, r.Err, nil
}

// ParseKey is the cache key of one parse result.
func ParseKey(blobSHA, parserID string, parserVersion int) string {
	return fmt.Sprintf("%s|%s|%d", blobSHA, parserID, parserVersion)
}

// wireLocked is the compact persisted form of one locked component (short keys, zero values omitted).
type wireLocked struct {
	Eco   EcosystemID  `json:"e"`
	Ns    string       `json:"ns,omitempty"`
	Name  string       `json:"n"`
	Ver   string       `json:"v,omitempty"`
	Rel   Relationship `json:"r,omitempty"`
	Scope Scope        `json:"sc,omitempty"`
	Int   string       `json:"i,omitempty"`
	Lic   []string     `json:"l,omitempty"`
	Dep   []int        `json:"d,omitempty"`
	Path  string       `json:"p,omitempty"`
	Line  int          `json:"ln,omitempty"`
	Src   string       `json:"s,omitempty"`
}

type lockedRecord struct {
	Schema int          `json:"schema"`
	Items  []wireLocked `json:"items"`
}

// maxLockedDecoded bounds what DecodeLocked will inflate (a corrupt or hostile cache entry must not become a memory bomb).
const maxLockedDecoded = 256 << 20

// EncodeLocked produces the value of one manifests.locked cache entry: compact JSON, gzip-compressed (a 600-component npm lockfile is about
// 60 KB instead of 220 KB; the 64 MiB default cap of the namespace then holds about 1,000 repositories, E-6).
func EncodeLocked(lc []LockedComponent) ([]byte, error) {
	rec := lockedRecord{Schema: ParseSchema, Items: make([]wireLocked, len(lc))}
	for i, l := range lc {
		rec.Items[i] = wireLocked{l.ID.Ecosystem, l.ID.Namespace, l.ID.Name, l.Version, l.Relationship, l.Scope, l.Integrity, l.Licenses, l.DependsOn, l.Loc.Path, l.Loc.Line, l.Source}
	}
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
	if err := json.NewEncoder(zw).Encode(rec); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeLocked reads a manifests.locked entry. Any error (not gzip, corrupt JSON, newer schema, too large) means "treat as a miss".
func DecodeLocked(b []byte) ([]LockedComponent, error) {
	zr, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var rec lockedRecord
	if err := json.NewDecoder(io.LimitReader(zr, maxLockedDecoded)).Decode(&rec); err != nil {
		return nil, err
	}
	if rec.Schema > ParseSchema {
		return nil, ErrSchema
	}
	out := make([]LockedComponent, len(rec.Items))
	for i, w := range rec.Items {
		out[i] = LockedComponent{ID: ComponentID{w.Eco, intern(w.Ns), intern(w.Name)}, Version: intern(w.Ver), Relationship: w.Rel, Scope: w.Scope, Integrity: w.Int, Licenses: w.Lic,
			DependsOn: w.Dep, Loc: Location{w.Path, w.Line}, Source: w.Src}
	}
	return out, nil
}
