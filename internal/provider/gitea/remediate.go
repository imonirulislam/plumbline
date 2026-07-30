package gitea

// This file implements the optional Remediator and FileRemediator capabilities
// for Gitea. Request/response shapes follow the Gitea OpenAPI spec
// (https://gitea.com/swagger.v1.json): branch_protections use rule_name,
// CreateBranch uses new_branch_name/old_ref_name, contents content is base64,
// and pulls have no head filter so we match on head.ref client-side.

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/imonirulislam/plumbline/internal/core"
)

// do performs a write request (POST/PATCH/…) and returns (status, body). Like
// get, it never errors on a non-2xx status — callers decide what each means.
func (c *Client) do(ctx context.Context, method, path string, reqBody []byte) (int, []byte, error) {
	var r io.Reader
	if reqBody != nil {
		r = bytes.NewReader(reqBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "token "+c.token)
	req.Header.Set("Accept", "application/json")
	if reqBody != nil {
		req.Header.Set("Content-Type", "application/json")
	}
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

func repoPath(ref core.RepoRef) string {
	return fmt.Sprintf("/repos/%s/%s", url.PathEscape(ref.Owner), url.PathEscape(ref.Name))
}

// ── Remediator ───────────────────────────────────────────────────────────────

// FixableChecks lists the checks this connector can remediate.
func (c *Client) FixableChecks() []string { return []string{"branch-protection", "default-branch"} }

// Fix remediates the named check for a repo.
func (c *Client) Fix(ctx context.Context, ref core.RepoRef, check string, pol core.Policy) error {
	switch check {
	case "branch-protection":
		return c.protectDefaultBranch(ctx, ref)
	case "default-branch":
		return c.setDefaultBranch(ctx, ref, pol.DefaultBranch)
	default:
		return fmt.Errorf("gitea: cannot fix %q", check)
	}
}

// currentDefaultBranch reads the repo's live default branch. branch-protection
// runs after default-branch (registry order), so the ref we were handed may be
// stale — always protect whatever is default now.
func (c *Client) currentDefaultBranch(ctx context.Context, ref core.RepoRef) (string, error) {
	status, body, err := c.get(ctx, repoPath(ref))
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

// protectDefaultBranch creates a branch-protection rule for the live default
// branch. Setting rule_name (and branch_name for older Gitea) to the exact
// branch makes the Branch object report protected: true, which Inspect reads.
func (c *Client) protectDefaultBranch(ctx context.Context, ref core.RepoRef) error {
	branch, err := c.currentDefaultBranch(ctx, ref)
	if err != nil {
		return err
	}
	if branch == "" {
		return errors.New("gitea: repo has no default branch to protect")
	}
	body, _ := json.Marshal(map[string]any{"rule_name": branch, "branch_name": branch})
	status, resp, err := c.do(ctx, http.MethodPost, repoPath(ref)+"/branch_protections", body)
	if err != nil {
		return err
	}
	if status == http.StatusConflict {
		return nil // a rule for this branch already exists
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("enable protection: HTTP %d: %s", status, snippet(resp))
	}
	return nil
}

// setDefaultBranch points the repo's default at target. Gitea has no atomic
// rename, so this creates target from the current default and switches the
// default pointer (the old branch is left in place). default-branch runs first
// in the registry, so ref.DefaultBranch is still current here.
func (c *Client) setDefaultBranch(ctx context.Context, ref core.RepoRef, target string) error {
	if target == "" {
		return errors.New("gitea: policy default branch is empty")
	}
	if ref.DefaultBranch == "" {
		return errors.New("gitea: repo has no default branch")
	}
	if ref.DefaultBranch == target {
		return nil
	}
	if err := c.createBranch(ctx, ref, target, ref.DefaultBranch); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"default_branch": target})
	status, resp, err := c.do(ctx, http.MethodPatch, repoPath(ref), body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("set default branch: HTTP %d: %s", status, snippet(resp))
	}
	return nil
}

func (c *Client) createBranch(ctx context.Context, ref core.RepoRef, newName, fromRef string) error {
	body, _ := json.Marshal(map[string]string{"new_branch_name": newName, "old_ref_name": fromRef})
	status, resp, err := c.do(ctx, http.MethodPost, repoPath(ref)+"/branches", body)
	if err != nil {
		return err
	}
	if status == http.StatusConflict {
		return nil // branch already exists — fine
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("create branch %s: HTTP %d: %s", newName, status, snippet(resp))
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
		return "", fmt.Errorf("gitea: cannot file-fix %q", check)
	}
}

func (c *Client) openRenovatePR(ctx context.Context, ref core.RepoRef) (string, error) {
	if ref.DefaultBranch == "" {
		return "", errors.New("gitea: repo has no default branch")
	}
	if u, ok, err := c.existingPR(ctx, ref, fixBranch); err != nil {
		return "", err
	} else if ok {
		return u, nil
	}
	if err := c.createBranch(ctx, ref, fixBranch, ref.DefaultBranch); err != nil {
		return "", err
	}
	if err := c.createFile(ctx, ref, fixBranch, "renovate.json", core.RenovateConfig(),
		"chore: add renovate.json"); err != nil {
		return "", err
	}
	return c.createPR(ctx, ref, fixBranch, ref.DefaultBranch,
		"chore: add renovate.json",
		"Adds a Renovate config so dependency updates are automated.\n\nOpened by plumbline.")
}

func (c *Client) createFile(ctx context.Context, ref core.RepoRef, branch, path string, content []byte, message string) error {
	body, _ := json.Marshal(map[string]string{
		"content": base64.StdEncoding.EncodeToString(content), // Gitea requires base64
		"branch":  branch,
		"message": message,
	})
	p := repoPath(ref) + "/contents/" + url.PathEscape(path)
	status, resp, err := c.do(ctx, http.MethodPost, p, body)
	if err != nil {
		return err
	}
	if status == http.StatusConflict || status == http.StatusUnprocessableEntity {
		return nil // file already exists on the branch — proceed to the PR
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("create %s: HTTP %d: %s", path, status, snippet(resp))
	}
	return nil
}

func (c *Client) createPR(ctx context.Context, ref core.RepoRef, head, base, title, prBody string) (string, error) {
	body, _ := json.Marshal(map[string]string{"title": title, "head": head, "base": base, "body": prBody})
	status, resp, err := c.do(ctx, http.MethodPost, repoPath(ref)+"/pulls", body)
	if err != nil {
		return "", err
	}
	if status == http.StatusConflict || status == http.StatusUnprocessableEntity {
		if u, ok, e := c.existingPR(ctx, ref, head); e == nil && ok {
			return u, nil
		}
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("open PR: HTTP %d: %s", status, snippet(resp))
	}
	var pr struct {
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(resp, &pr); err != nil {
		return "", err
	}
	return pr.HTMLURL, nil
}

// existingPR finds an open PR whose head branch is headRef. Gitea's pulls list
// has no head filter, so we page open PRs and match head.ref ourselves.
func (c *Client) existingPR(ctx context.Context, ref core.RepoRef, headRef string) (string, bool, error) {
	status, body, err := c.get(ctx, repoPath(ref)+"/pulls?state=open&limit=50")
	if err != nil {
		return "", false, err
	}
	if status != http.StatusOK {
		return "", false, fmt.Errorf("list PRs: HTTP %d: %s", status, snippet(body))
	}
	var prs []struct {
		HTMLURL string `json:"html_url"`
		Head    struct {
			Ref string `json:"ref"`
		} `json:"head"`
	}
	if err := json.Unmarshal(body, &prs); err != nil {
		return "", false, err
	}
	for _, pr := range prs {
		if pr.Head.Ref == headRef {
			return pr.HTMLURL, true, nil
		}
	}
	return "", false, nil
}
