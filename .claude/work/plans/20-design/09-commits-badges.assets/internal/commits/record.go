package commits

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/cantara/source-code-portal/internal/scm"
	"github.com/cantara/source-code-portal/internal/store"
)

const ringSchema = 1

// ringRecord is the payload of the commits.ring item: the authoritative membership of the ring. commit_log keeps a
// union of everything ever appended (AppendCommits is insert-or-ignore plus trim), so after a force push it may hold
// commits that are no longer on the branch; Restore therefore loads only the SHAs listed here.
type ringRecord struct {
	Schema int      `json:"schema"`
	Branch string   `json:"branch"`
	Head   string   `json:"head"`
	SHAs   []string `json:"shas"` // ring order: newest first
}

// rowRecord is the payload of one commit_log row; SHA and CommittedAt live in the row columns.
type rowRecord struct {
	V       int    `json:"v"`
	Subject string `json:"s,omitempty"`
	Login   string `json:"l,omitempty"`
	Name    string `json:"n,omitempty"`
	URL     string `json:"u,omitempty"`
}

func encodeRows(cs []scm.Commit) ([]store.CommitRow, error) {
	rows := make([]store.CommitRow, 0, len(cs))
	for _, c := range cs {
		p, err := json.Marshal(rowRecord{V: 1, Subject: c.Subject, Login: c.AuthorLogin, Name: c.AuthorName, URL: c.URL})
		if err != nil {
			return nil, err
		}
		rows = append(rows, store.CommitRow{SHA: c.SHA, CommittedAt: c.CommittedAt, Payload: p})
	}
	return rows, nil
}

func decodeRow(r store.CommitRow) (scm.Commit, error) {
	var rr rowRecord
	if err := json.Unmarshal(r.Payload, &rr); err != nil {
		return scm.Commit{}, err
	}
	if rr.V != 1 {
		return scm.Commit{}, fmt.Errorf("commits: unknown row schema %d", rr.V)
	}
	return scm.Commit{SHA: r.SHA, Subject: rr.Subject, AuthorLogin: rr.Login, AuthorName: rr.Name, URL: rr.URL, CommittedAt: r.CommittedAt.UTC()}, nil
}

func encodeRing(branch, head string, cs []scm.Commit) ([]byte, error) {
	shas := make([]string, len(cs))
	for i, c := range cs {
		shas[i] = c.SHA
	}
	return json.Marshal(ringRecord{Schema: ringSchema, Branch: branch, Head: head, SHAs: shas})
}

func decodeRing(p []byte) (ringRecord, error) {
	var rr ringRecord
	if err := json.Unmarshal(p, &rr); err != nil {
		return rr, err
	}
	if rr.Schema != ringSchema {
		return rr, fmt.Errorf("commits: unknown ring schema %d", rr.Schema)
	}
	return rr, nil
}

var _ = time.Second
