package github

// This file implements the optional Remediator (settings fixes) and
// FileRemediator (PR-based fixes) capabilities for GitHub. The read model lives
// in github.go; the write path lives here so each connector's file split is
// uniform (<connector>.go = read, remediate.go = write).

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/imonirulislam/plumbline/internal/core"
)

// ── Remediator ───────────────────────────────────────────────────────────────

// FixableChecks lists the checks this connector can remediate.
func (c *Client) FixableChecks() []string { return []string{"branch-protection", "default-branch"} }

// Fix remediates the named check for a repo.
func (c *Client) Fix(ctx context.Context, ref core.RepoRef, check string, pol core.Policy) error {
	switch check {
	case "branch-protection":
		return c.protectDefaultBranch(ctx, ref)
	case "default-branch":
		return c.renameDefaultBranch(ctx, ref, pol.DefaultBranch)
	default:
		return fmt.Errorf("github: cannot fix %q", check)
	}
}

// renameDefaultBranch renames the repo's current default branch to target.
// GitHub's rename endpoint moves the default pointer and retargets open PRs.
// default-branch runs first in the registry, so ref.DefaultBranch is current.
func (c *Client) renameDefaultBranch(ctx context.Context, ref core.RepoRef, target string) error {
	if target == "" {
		return errors.New("policy default branch is empty")
	}
	if ref.DefaultBranch == "" {
		return errors.New("repo has no default branch to rename")
	}
	if ref.DefaultBranch == target {
		return nil
	}
	body, err := json.Marshal(map[string]string{"new_name": target})
	if err != nil {
		return err
	}
	p := fmt.Sprintf("/repos/%s/%s/branches/%s/rename",
		url.PathEscape(ref.Owner), url.PathEscape(ref.Name), url.PathEscape(ref.DefaultBranch))
	status, respBody, _, err := c.do(ctx, http.MethodPost, p, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("rename branch: HTTP %d: %s", status, snippet(respBody))
	}
	return nil
}

// currentDefaultBranch reads the repo's live default branch. branch-protection
// runs after default-branch (registry order), so the ref we were handed may be
// stale — always protect whatever is default now.
func (c *Client) currentDefaultBranch(ctx context.Context, ref core.RepoRef) (string, error) {
	p := fmt.Sprintf("/repos/%s/%s", url.PathEscape(ref.Owner), url.PathEscape(ref.Name))
	status, body, _, err := c.get(ctx, p)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("get repo: HTTP %d: %s", status, snippet(body))
	}
	var r struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", err
	}
	return r.DefaultBranch, nil
}

// protectDefaultBranch enables a minimal protection rule on the live default
// branch: no required reviews/checks (so it won't block a solo maintainer), but
// force pushes and deletions are disabled — enough to mark the branch protected.
func (c *Client) protectDefaultBranch(ctx context.Context, ref core.RepoRef) error {
	branch, err := c.currentDefaultBranch(ctx, ref)
	if err != nil {
		return err
	}
	if branch == "" {
		return errors.New("no default branch to protect")
	}
	body := []byte(`{` +
		`"required_status_checks":null,` +
		`"enforce_admins":false,` +
		`"required_pull_request_reviews":null,` +
		`"restrictions":null,` +
		`"allow_force_pushes":false,` +
		`"allow_deletions":false}`)
	p := fmt.Sprintf("/repos/%s/%s/branches/%s/protection",
		url.PathEscape(ref.Owner), url.PathEscape(ref.Name), url.PathEscape(branch))
	status, respBody, _, err := c.do(ctx, http.MethodPut, p, body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("enable protection: HTTP %d: %s", status, snippet(respBody))
	}
	return nil
}

// ── FileRemediator ───────────────────────────────────────────────────────────

const fixBranch = "plumbline/dependency-automation"

// FileFixableChecks lists checks this connector can fix by opening a PR.
func (c *Client) FileFixableChecks() []string { return []string{"dependency-automation"} }

// OpenFix opens (or reuses) a PR remediating check for ref.
func (c *Client) OpenFix(ctx context.Context, ref core.RepoRef, check string) (string, error) {
	switch check {
	case "dependency-automation":
		return c.openRenovatePR(ctx, ref)
	default:
		return "", fmt.Errorf("github: cannot file-fix %q", check)
	}
}

func (c *Client) openRenovatePR(ctx context.Context, ref core.RepoRef) (string, error) {
	if ref.DefaultBranch == "" {
		return "", errors.New("repo has no default branch")
	}
	// Idempotency: reuse an already-open plumbline PR.
	if url, ok, err := c.existingPR(ctx, ref, fixBranch); err != nil {
		return "", err
	} else if ok {
		return url, nil
	}
	sha, err := c.branchSHA(ctx, ref, ref.DefaultBranch)
	if err != nil {
		return "", err
	}
	if err := c.createRef(ctx, ref, fixBranch, sha); err != nil {
		return "", err
	}
	if err := c.putFile(ctx, ref, fixBranch, "renovate.json", core.RenovateConfig(),
		"chore: add renovate.json"); err != nil {
		return "", err
	}
	return c.createPR(ctx, ref, fixBranch, ref.DefaultBranch,
		"chore: add renovate.json",
		"Adds a Renovate config so dependency updates are automated.\n\nOpened by plumbline.")
}

func (c *Client) existingPR(ctx context.Context, ref core.RepoRef, branch string) (string, bool, error) {
	p := fmt.Sprintf("/repos/%s/%s/pulls?state=open&head=%s:%s",
		url.PathEscape(ref.Owner), url.PathEscape(ref.Name), url.QueryEscape(ref.Owner), url.QueryEscape(branch))
	status, body, _, err := c.get(ctx, p)
	if err != nil {
		return "", false, err
	}
	if status != http.StatusOK {
		return "", false, fmt.Errorf("list PRs: HTTP %d: %s", status, snippet(body))
	}
	var prs []struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(body, &prs); err != nil {
		return "", false, err
	}
	if len(prs) > 0 {
		return prs[0].HTMLURL, true, nil
	}
	return "", false, nil
}

func (c *Client) branchSHA(ctx context.Context, ref core.RepoRef, branch string) (string, error) {
	p := fmt.Sprintf("/repos/%s/%s/git/ref/heads/%s",
		url.PathEscape(ref.Owner), url.PathEscape(ref.Name), url.PathEscape(branch))
	status, body, _, err := c.get(ctx, p)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("get ref %s: HTTP %d: %s", branch, status, snippet(body))
	}
	var r struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", err
	}
	return r.Object.SHA, nil
}

func (c *Client) createRef(ctx context.Context, ref core.RepoRef, branch, sha string) error {
	body, _ := json.Marshal(map[string]string{"ref": "refs/heads/" + branch, "sha": sha})
	p := fmt.Sprintf("/repos/%s/%s/git/refs", url.PathEscape(ref.Owner), url.PathEscape(ref.Name))
	status, respBody, _, err := c.do(ctx, http.MethodPost, p, body)
	if err != nil {
		return err
	}
	if status == http.StatusUnprocessableEntity {
		return nil // branch already exists — fine, we'll commit onto it
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("create branch %s: HTTP %d: %s", branch, status, snippet(respBody))
	}
	return nil
}

func (c *Client) putFile(ctx context.Context, ref core.RepoRef, branch, path string, content []byte, message string) error {
	body, _ := json.Marshal(map[string]string{
		"message": message,
		"content": base64.StdEncoding.EncodeToString(content),
		"branch":  branch,
	})
	enc := url.PathEscape(path)
	p := fmt.Sprintf("/repos/%s/%s/contents/%s", url.PathEscape(ref.Owner), url.PathEscape(ref.Name), enc)
	status, respBody, _, err := c.do(ctx, http.MethodPut, p, body)
	if err != nil {
		return err
	}
	if status == http.StatusUnprocessableEntity {
		return nil // file already exists on the branch — proceed to the PR
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("put %s: HTTP %d: %s", path, status, snippet(respBody))
	}
	return nil
}

func (c *Client) createPR(ctx context.Context, ref core.RepoRef, head, base, title, prBody string) (string, error) {
	body, _ := json.Marshal(map[string]string{"title": title, "head": head, "base": base, "body": prBody})
	p := fmt.Sprintf("/repos/%s/%s/pulls", url.PathEscape(ref.Owner), url.PathEscape(ref.Name))
	status, respBody, _, err := c.do(ctx, http.MethodPost, p, body)
	if err != nil {
		return "", err
	}
	if status == http.StatusUnprocessableEntity {
		// A PR for this branch may already exist — return it.
		if u, ok, e := c.existingPR(ctx, ref, head); e == nil && ok {
			return u, nil
		}
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("open PR: HTTP %d: %s", status, snippet(respBody))
	}
	var pr struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(respBody, &pr); err != nil {
		return "", err
	}
	return pr.HTMLURL, nil
}
