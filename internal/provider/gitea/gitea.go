// Package gitea is the Gitea connector: it adapts the Gitea REST API (also
// serves Forgejo) into plumbline's normalized model using only the standard
// library. It self-registers as "gitea"; import it for its side effect:
//
//	import _ "github.com/imonirulislam/plumbline/internal/provider/gitea"
//
// Gitea is self-hosted, so a base URL is required (e.g. https://gitea.example.com).
package gitea

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/imonirulislam/plumbline/internal/core"
	"github.com/imonirulislam/plumbline/internal/provider"
)

const apiSuffix = "/api/v1"

func init() {
	provider.Register("gitea", func(cfg provider.Config) (provider.Provider, error) {
		if cfg.BaseURL == "" {
			return nil, errors.New("gitea: --base-url is required (e.g. https://gitea.example.com)")
		}
		if cfg.Token == "" {
			return nil, errors.New("gitea: no token (set PLUMBLINE_TOKEN)")
		}
		base := strings.TrimRight(cfg.BaseURL, "/")
		if !strings.Contains(base, apiSuffix) {
			base += apiSuffix
		}
		return &Client{
			http:  &http.Client{Timeout: 30 * time.Second},
			base:  base,
			token: cfg.Token,
		}, nil
	})
}

// Client is the Gitea connector.
type Client struct {
	http  *http.Client
	base  string
	token string
}

// Name returns the connector's registered name.
func (c *Client) Name() string { return "gitea" }

func (c *Client) get(ctx context.Context, path string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, body, nil
}

// ListRepos lists non-fork repos owned by owner (user or org), paginated.
func (c *Client) ListRepos(ctx context.Context, owner string) ([]core.RepoRef, error) {
	type giteaRepo struct {
		Name          string `json:"name"`
		Private       bool   `json:"private"`
		Archived      bool   `json:"archived"`
		Fork          bool   `json:"fork"`
		DefaultBranch string `json:"default_branch"`
		HTMLURL       string `json:"html_url"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	}
	const limit = 50
	var out []core.RepoRef
	for page := 1; ; page++ {
		p := fmt.Sprintf("/users/%s/repos?limit=%d&page=%d", url.PathEscape(owner), limit, page)
		status, body, err := c.get(ctx, p)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("list repos for %q: HTTP %d: %s", owner, status, snippet(body))
		}
		var pageRepos []giteaRepo
		if err := json.Unmarshal(body, &pageRepos); err != nil {
			return nil, fmt.Errorf("list repos: decode: %w", err)
		}
		for _, r := range pageRepos {
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
		if len(pageRepos) < limit {
			break
		}
	}
	return out, nil
}

// Inspect gathers the normalized facts checks need.
func (c *Client) Inspect(ctx context.Context, ref core.RepoRef) (core.RepoState, error) {
	st := core.RepoState{Ref: ref}

	prot, err := c.branchProtected(ctx, ref)
	if err != nil {
		return st, err
	}
	st.DefaultBranchProtected = prot

	// CI: Gitea Actions (.gitea/workflows or .github/workflows), or Drone/Woodpecker.
	st.HasCI = core.TriNo
	for _, dir := range []string{".gitea/workflows", ".github/workflows"} {
		if c.dirHasEntries(ctx, ref, dir) == core.TriYes {
			st.HasCI = core.TriYes
			break
		}
	}
	if st.HasCI != core.TriYes {
		st.HasCI = c.anyPathExists(ctx, ref, []string{".drone.yml", ".woodpecker.yml"})
	}

	// Dependency automation: Renovate (self-hosted Renovate supports Gitea).
	st.HasDependencyAutomation = c.anyPathExists(ctx, ref, []string{
		"renovate.json",
		".renovaterc.json",
		".github/renovate.json",
		".gitea/renovate.json",
	})

	return st, nil
}

func (c *Client) branchProtected(ctx context.Context, ref core.RepoRef) (core.Tri, error) {
	if ref.DefaultBranch == "" {
		return core.TriUnknown, nil
	}
	p := fmt.Sprintf("/repos/%s/%s/branches/%s",
		url.PathEscape(ref.Owner), url.PathEscape(ref.Name), url.PathEscape(ref.DefaultBranch))
	status, body, err := c.get(ctx, p)
	if err != nil {
		return core.TriUnknown, err
	}
	if status == http.StatusNotFound {
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

func (c *Client) dirHasEntries(ctx context.Context, ref core.RepoRef, dir string) core.Tri {
	status, body, err := c.get(ctx, c.contentsPath(ref, dir))
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

func (c *Client) anyPathExists(ctx context.Context, ref core.RepoRef, paths []string) core.Tri {
	for _, path := range paths {
		status, _, err := c.get(ctx, c.contentsPath(ref, path))
		if err != nil {
			return core.TriUnknown
		}
		if status == http.StatusOK {
			return core.TriYes
		}
	}
	return core.TriNo
}

func (c *Client) contentsPath(ref core.RepoRef, path string) string {
	p := fmt.Sprintf("/repos/%s/%s/contents/%s",
		url.PathEscape(ref.Owner), url.PathEscape(ref.Name), path)
	if ref.DefaultBranch != "" {
		p += "?ref=" + url.QueryEscape(ref.DefaultBranch)
	}
	return p
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 160 {
		return s[:160]
	}
	return s
}
