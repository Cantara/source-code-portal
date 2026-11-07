package setup

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/cantara/source-code-portal/internal/ghlogin"
)

// Job states.
const (
	jobRunning   = "running"
	jobDone      = "done"
	jobFailed    = "failed"
	jobCancelled = "cancelled"
)

// listing is the background job that reads the repositories of the chosen accounts, one account after the other. The page polls
// its progress: how many repositories are in, out of how many the accounts are said to hold.
type listing struct {
	id       int // told apart from an older job still finishing
	state    string
	accounts []string
	counting bool   // the accounts' sizes are being asked: the bar has no total yet
	account  string // being read
	step     int    // 1-based index of that account
	done     int    // repositories read, over all accounts
	total    int    // repositories expected, over all accounts (0: not known)
	err      string
	cancel   context.CancelFunc
}

type jobOut struct {
	State    string `json:"state"`
	Counting bool   `json:"counting,omitempty"`
	Account  string `json:"account,omitempty"`
	Step     int    `json:"step"`
	Steps    int    `json:"steps"`
	Done     int    `json:"done"`
	Total    int    `json:"total"` // 0: not known, the bar is indeterminate
	Error    string `json:"error,omitempty"`
}

func (l *listing) out() *jobOut {
	if l == nil {
		return nil
	}
	return &jobOut{State: l.state, Counting: l.counting, Account: l.account, Step: l.step, Steps: len(l.accounts), Done: l.done, Total: l.total, Error: l.err}
}

// startListing begins reading the repositories of the accounts the person chose (or answers at once when they are already read).
func (s *Server) startListing(_ context.Context, body []byte) (any, error) {
	var in struct {
		Accounts []string `json:"accounts"`
	}
	if err := decode(body, &in); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token.Value == "" {
		return nil, bad("Sign in first.")
	}
	accounts, err := s.chosen(in.Accounts)
	if err != nil {
		return nil, err
	}
	slices.Sort(accounts)
	if s.job != nil && s.job.state == jobRunning {
		if slices.Equal(s.job.accounts, accounts) {
			return s.snapshot(), nil
		}
		s.job.cancel() // another choice: the running read is not wanted
	}
	if s.listed && slices.Equal(s.listedFor, accounts) {
		return s.snapshot(), nil
	}
	s.jobs++
	ctx, cancel := context.WithCancel(s.ctx)
	s.job = &listing{id: s.jobs, state: jobRunning, counting: true, accounts: accounts, step: 1, account: accounts[0], cancel: cancel}
	s.listed, s.repos, s.more = false, nil, false
	s.wg.Add(1)
	go s.readAccounts(ctx, s.job.id, s.client(), s.token.Value, s.user, accounts)
	return s.snapshot(), nil
}

// cancelListing stops the read in progress; the page goes back to the accounts.
func (s *Server) cancelListing(_ context.Context, _ []byte) (any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.job != nil && s.job.state == jobRunning {
		s.job.cancel()
		s.job.state = jobCancelled
	}
	return s.snapshot(), nil
}

// readAccounts reads the accounts in turn: first the size each says it has (the bar's total), then the repositories page by page.
func (s *Server) readAccounts(ctx context.Context, id int, c *ghlogin.Client, tok, user string, accounts []string) {
	defer s.wg.Done()
	current := func() *listing { // callers hold s.mu
		if s.job != nil && s.job.id == id && s.job.state == jobRunning {
			return s.job
		}
		return nil
	}
	fail := func(err error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if j := current(); j != nil && ctx.Err() == nil {
			j.state, j.err = jobFailed, fmt.Sprintf("Reading %s failed: %v", j.account, err)
		}
	}
	self := func(a string) bool { return strings.EqualFold(a, user) }
	sizes := make([]int, len(accounts))
	var counts errgroup.Group
	counts.SetLimit(5)
	for i, a := range accounts {
		counts.Go(func() error {
			if n, err := c.AccountSize(ctx, tok, a, self(a)); err == nil { // the size is a hint; an unknown one only makes the bar less exact
				sizes[i] = n
			}
			return nil
		})
	}
	_ = counts.Wait()
	total := 0
	for _, n := range sizes {
		total += n
	}
	s.mu.Lock()
	if j := current(); j != nil {
		j.total, j.counting = total, false
	}
	s.mu.Unlock()

	var all []ghlogin.Repo
	more := false
	base := 0
	for i, a := range accounts {
		s.mu.Lock()
		if j := current(); j != nil {
			j.account, j.step = a, i+1
		}
		s.mu.Unlock()
		repos, m, err := c.AccountRepos(ctx, tok, a, self(a), func(n int) {
			s.mu.Lock()
			defer s.mu.Unlock()
			if j := current(); j != nil {
				j.done = base + n
				j.total = max(j.total, j.done) // a size that was too low must not push the bar past its end
			}
		})
		if err != nil {
			fail(err)
			return
		}
		more = more || m
		all = append(all, repos...)
		base += len(repos)
	}
	slices.SortStableFunc(all, func(a, b ghlogin.Repo) int { return b.Pushed.Compare(a.Pushed) }) // the most recently pushed first
	s.mu.Lock()
	defer s.mu.Unlock()
	if j := current(); j != nil {
		j.state, j.done, j.total = jobDone, len(all), len(all)
		s.repos, s.more, s.listed, s.listedFor = all, more, true, accounts
	}
}
