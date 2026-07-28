// Package github is the GitHub connector: it adapts the GitHub REST API into
// plumbline's normalized model using only the standard library. It self-
// registers as "github"; import it for its side effect:
//
//	import _ "github.com/imonirulislam/plumbline/internal/provider/github"
package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/imonirulislam/plumbline/internal/core"
	"github.com/imonirulislam/plumbline/internal/provider"
)

const defaultBaseURL = "https://api.github.com"

func init() {
	provider.Register("github", func(cfg provider.Config) (provider.Provider, error) {
		if cfg.Token == "" {
			return nil, errors.New("github: no token (set GITHUB_TOKEN)")
		}
		base := cfg.BaseURL
		if base == "" {
			base = defaultBaseURL
		}
		return &Client{
			http:  &http.Client{Timeout: 30 * time.Second},
			base:  strings.TrimRight(base, "/"),
			token: cfg.Token,
		}, nil
	})
}

// Client is the GitHub connector.
type Client struct {
	http  *http.Client
	base  string
	token string
}

func (c *Client) Name() string { return "github" }

// get performs a GET and returns (status, body). It never returns an error for
// non-2xx statuses — callers decide what a 404 etc. means.
func (c *Client) get(ctx context.Context, path string) (int, []byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return 0, nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, resp.Header, err
	}
	return resp.StatusCode, body, resp.Header, nil
}

var linkNextRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

// ListRepos lists non-fork source repos owned by owner, following pagination.
func (c *Client) ListRepos(ctx context.Context, owner string) ([]core.RepoRef, error) {
	type ghRepo struct {
		Name          string `json:"name"`
		FullName      string `json:"full_name"`
		Private       bool   `json:"private"`
		Archived      bool   `json:"archived"`
		Fork          bool   `json:"fork"`
		HTMLURL       string `json:"html_url"`
		DefaultBranch string `json:"default_branch"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	path := "/users/" + url.PathEscape(owner) + "/repos?per_page=100&type=owner&sort=full_name"
	var out []core.RepoRef
	for path != "" {
		status, body, hdr, err := c.get(ctx, path)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("list repos for %q: HTTP %d: %s", owner, status, snippet(body))
		}
		var page []ghRepo
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, fmt.Errorf("list repos: decode: %w", err)
		}
		for _, r := range page {
			if r.Fork {
				continue
			}
			out = append(out, core.RepoRef{
				Owner:         r.Owner.Login,
				Name:          r.Name,
				DefaultBranch: r.DefaultBranch,
				Private:       r.Private,
				Archived:      r.Archived,
				URL:           r.HTMLURL,
			})
		}
		path = nextLink(hdr)
	}
	return out, nil
}

// Inspect gathers the normalized facts checks need.
func (c *Client) Inspect(ctx context.Context, ref core.RepoRef) (core.RepoState, error) {
	st := core.RepoState{Ref: ref}

	// Default-branch protection: the branch object exposes a "protected" bool.
	prot, err := c.branchProtected(ctx, ref)
	if err != nil {
		return st, err
	}
	st.DefaultBranchProtected = prot

	// CI: any workflow file under .github/workflows.
	st.HasCI = c.dirHasEntries(ctx, ref, ".github/workflows")

	// Dependency automation: Dependabot or Renovate config in a known location.
	st.HasDependencyAutomation = c.anyPathExists(ctx, ref, []string{
		".github/dependabot.yml",
		".github/dependabot.yaml",
		"renovate.json",
		".renovaterc.json",
		".github/renovate.json",
	})

	return st, nil
}

func (c *Client) branchProtected(ctx context.Context, ref core.RepoRef) (core.Tri, error) {
	if ref.DefaultBranch == "" {
		return core.TriUnknown, nil
	}
	p := fmt.Sprintf("/repos/%s/%s/branches/%s",
		url.PathEscape(ref.Owner), url.PathEscape(ref.Name), url.PathEscape(ref.DefaultBranch))
	status, body, _, err := c.get(ctx, p)
	if err != nil {
		return core.TriUnknown, err
	}
	if status == http.StatusNotFound {
		// Branch doesn't exist (e.g. an empty repo) — treat as "not protected"
		// rather than hard-erroring the whole repo's audit.
		return core.TriNo, nil
	}
	if status != http.StatusOK {
		return core.TriUnknown, fmt.Errorf("branch %s: HTTP %d: %s", ref.DefaultBranch, status, snippet(body))
	}
	var b struct {
		Protected bool `json:"protected"`
	}
	if err := json.Unmarshal(body, &b); err != nil {
		return core.TriUnknown, err
	}
	if b.Protected {
		return core.TriYes, nil
	}
	return core.TriNo, nil
}

// dirHasEntries reports whether a directory exists and is non-empty.
func (c *Client) dirHasEntries(ctx context.Context, ref core.RepoRef, dir string) core.Tri {
	p := fmt.Sprintf("/repos/%s/%s/contents/%s",
		url.PathEscape(ref.Owner), url.PathEscape(ref.Name), dir)
	status, body, _, err := c.get(ctx, p)
	if err != nil || status == http.StatusNotFound {
		return core.TriNo
	}
	if status != http.StatusOK {
		return core.TriUnknown
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(body, &entries); err != nil {
		return core.TriNo // a file, not a directory
	}
	if len(entries) > 0 {
		return core.TriYes
	}
	return core.TriNo
}

// anyPathExists reports whether any of the given file paths exist.
func (c *Client) anyPathExists(ctx context.Context, ref core.RepoRef, paths []string) core.Tri {
	for _, path := range paths {
		p := fmt.Sprintf("/repos/%s/%s/contents/%s",
			url.PathEscape(ref.Owner), url.PathEscape(ref.Name), path)
		status, _, _, err := c.get(ctx, p)
		if err != nil {
			return core.TriUnknown
		}
		if status == http.StatusOK {
			return core.TriYes
		}
	}
	return core.TriNo
}

func nextLink(h http.Header) string {
	m := linkNextRe.FindStringSubmatch(h.Get("Link"))
	if len(m) == 2 {
		return strings.TrimPrefix(m[1], defaultBaseURL)
	}
	return ""
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 160 {
		return s[:160]
	}
	return s
}
