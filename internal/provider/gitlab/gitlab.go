// Package gitlab is the GitLab connector: it adapts the GitLab REST API (v4)
// into plumbline's normalized model using only the standard library. It self-
// registers as "gitlab"; import it for its side effect:
//
//	import _ "github.com/imonirulislam/plumbline/internal/provider/gitlab"
//
// Defaults to https://gitlab.com; pass --base-url for self-hosted instances.
package gitlab

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

const (
	defaultBaseURL = "https://gitlab.com"
	apiSuffix      = "/api/v4"
)

func init() {
	provider.Register("gitlab", func(cfg provider.Config) (provider.Provider, error) {
		if cfg.Token == "" {
			return nil, errors.New("gitlab: no token (set PLUMBLINE_TOKEN)")
		}
		base := cfg.BaseURL
		if base == "" {
			base = defaultBaseURL
		}
		base = strings.TrimRight(base, "/")
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

// Client is the GitLab connector.
type Client struct {
	http  *http.Client
	base  string
	token string
}

// Name returns the connector's registered name.
func (c *Client) Name() string { return "gitlab" }

func (c *Client) get(ctx context.Context, path string) (int, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
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

type glProject struct {
	Path              string          `json:"path"`
	DefaultBranch     string          `json:"default_branch"`
	Visibility        string          `json:"visibility"`
	Archived          bool            `json:"archived"`
	WebURL            string          `json:"web_url"`
	ForkedFromProject json.RawMessage `json:"forked_from_project"`
	Namespace         struct {
		FullPath string `json:"full_path"`
	} `json:"namespace"`
}

func (p glProject) isFork() bool {
	return len(p.ForkedFromProject) > 0 && string(p.ForkedFromProject) != "null"
}

// ListRepos lists non-fork projects under owner. GitLab namespaces may be a
// group or a user, so it tries the group endpoint first and falls back to the
// user endpoint.
func (c *Client) ListRepos(ctx context.Context, owner string) ([]core.RepoRef, error) {
	esc := url.PathEscape(owner)
	group := "/groups/" + esc + "/projects?include_subgroups=true&with_shared=false&archived=false"
	if repos, ok, err := c.listFrom(ctx, group); err != nil {
		return nil, err
	} else if ok {
		return repos, nil
	}
	user := "/users/" + esc + "/projects?archived=false"
	repos, ok, err := c.listFrom(ctx, user)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("owner %q not found as a group or user", owner)
	}
	return repos, nil
}

// listFrom paginates a projects endpoint. ok=false means 404 (try the fallback).
func (c *Client) listFrom(ctx context.Context, base string) (repos []core.RepoRef, ok bool, err error) {
	sep := "&"
	for page := 1; ; page++ {
		status, body, e := c.get(ctx, fmt.Sprintf("%s%sper_page=100&page=%d", base, sep, page))
		if e != nil {
			return nil, false, e
		}
		if status == http.StatusNotFound {
			return nil, false, nil
		}
		if status != http.StatusOK {
			return nil, false, fmt.Errorf("list projects: HTTP %d: %s", status, snippet(body))
		}
		var page1 []glProject
		if err := json.Unmarshal(body, &page1); err != nil {
			return nil, false, fmt.Errorf("list projects: decode: %w", err)
		}
		for _, p := range page1 {
			if p.isFork() {
				continue
			}
			repos = append(repos, core.RepoRef{
				Owner:         p.Namespace.FullPath,
				Name:          p.Path,
				DefaultBranch: p.DefaultBranch,
				Private:       p.Visibility != "public",
				Archived:      p.Archived,
				URL:           p.WebURL,
			})
		}
		if len(page1) < 100 {
			break
		}
	}
	return repos, true, nil
}

// Inspect gathers the normalized facts checks need.
func (c *Client) Inspect(ctx context.Context, ref core.RepoRef) (core.RepoState, error) {
	st := core.RepoState{Ref: ref}

	prot, err := c.branchProtected(ctx, ref)
	if err != nil {
		return st, err
	}
	st.DefaultBranchProtected = prot

	// GitLab CI is a single root file, .gitlab-ci.yml.
	st.HasCI = c.anyFileExists(ctx, ref, []string{".gitlab-ci.yml"})

	// Dependency automation via Renovate (GitLab has no native Dependabot).
	st.HasDependencyAutomation = c.anyFileExists(ctx, ref, []string{
		"renovate.json",
		"renovate.json5",
		".renovaterc.json",
		".gitlab/renovate.json",
	})

	return st, nil
}

// projectID is the URL-encoded "namespace/path" GitLab uses to identify a project.
func projectID(ref core.RepoRef) string {
	return url.QueryEscape(ref.Owner + "/" + ref.Name)
}

func (c *Client) branchProtected(ctx context.Context, ref core.RepoRef) (core.Tri, error) {
	if ref.DefaultBranch == "" {
		return core.TriUnknown, nil
	}
	p := fmt.Sprintf("/projects/%s/repository/branches/%s", projectID(ref), url.PathEscape(ref.DefaultBranch))
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

func (c *Client) fileExists(ctx context.Context, ref core.RepoRef, path string) bool {
	enc := strings.ReplaceAll(url.PathEscape(path), "/", "%2F")
	p := fmt.Sprintf("/projects/%s/repository/files/%s", projectID(ref), enc)
	if ref.DefaultBranch != "" {
		p += "?ref=" + url.QueryEscape(ref.DefaultBranch)
	}
	status, _, err := c.get(ctx, p)
	return err == nil && status == http.StatusOK
}

func (c *Client) anyFileExists(ctx context.Context, ref core.RepoRef, paths []string) core.Tri {
	for _, path := range paths {
		if c.fileExists(ctx, ref, path) {
			return core.TriYes
		}
	}
	return core.TriNo
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 160 {
		return s[:160]
	}
	return s
}
