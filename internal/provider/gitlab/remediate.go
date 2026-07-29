package gitlab

// This file implements the optional Remediator and FileRemediator capabilities
// for GitLab. Request shapes follow the GitLab REST API docs
// (https://docs.gitlab.com/api/): protected_branches POST with name, branches
// POST with branch+ref, repository_files POST with RAW content (base64 only via
// an explicit encoding field — so we send raw), Edit project PUT with
// default_branch, and merge_requests filtered by state+source_branch.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/imonirulislam/plumbline/internal/core"
)

// do performs a write request and returns (status, body). Like get, it never
// errors on a non-2xx status — callers decide what each means.
func (c *Client) do(ctx context.Context, method, path string, reqBody []byte) (int, []byte, error) {
	var r io.Reader
	if reqBody != nil {
		r = bytes.NewReader(reqBody)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, r)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", c.token)
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

func projectPath(ref core.RepoRef) string { return "/projects/" + projectID(ref) }

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
		return fmt.Errorf("gitlab: cannot fix %q", check)
	}
}

// currentDefaultBranch reads the project's live default branch. branch-protection
// runs after default-branch (registry order), so the ref we were handed may be
// stale — always protect whatever is default now.
func (c *Client) currentDefaultBranch(ctx context.Context, ref core.RepoRef) (string, error) {
	status, body, err := c.get(ctx, projectPath(ref))
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("get project: HTTP %d: %s", status, snippet(body))
	}
	var p struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.Unmarshal(body, &p); err != nil {
		return "", err
	}
	return p.DefaultBranch, nil
}

// protectDefaultBranch adds a protected-branch entry for the live default
// branch, which makes the branch object report protected: true (what Inspect
// reads). Access levels default to Maintainer.
func (c *Client) protectDefaultBranch(ctx context.Context, ref core.RepoRef) error {
	branch, err := c.currentDefaultBranch(ctx, ref)
	if err != nil {
		return err
	}
	if branch == "" {
		return errors.New("gitlab: repo has no default branch to protect")
	}
	body, _ := json.Marshal(map[string]string{"name": branch})
	status, resp, err := c.do(ctx, http.MethodPost, projectPath(ref)+"/protected_branches", body)
	if err != nil {
		return err
	}
	if status == http.StatusConflict || status == http.StatusUnprocessableEntity {
		return nil // already protected
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("enable protection: HTTP %d: %s", status, snippet(resp))
	}
	return nil
}

// setDefaultBranch points the project's default at target. GitLab has no atomic
// rename, so this creates target from the current default and switches the
// default pointer (the old branch is left in place). default-branch runs first
// in the registry, so ref.DefaultBranch is still current here.
func (c *Client) setDefaultBranch(ctx context.Context, ref core.RepoRef, target string) error {
	if target == "" {
		return errors.New("gitlab: policy default branch is empty")
	}
	if ref.DefaultBranch == "" {
		return errors.New("gitlab: repo has no default branch")
	}
	if ref.DefaultBranch == target {
		return nil
	}
	if err := c.createBranch(ctx, ref, target, ref.DefaultBranch); err != nil {
		return err
	}
	body, _ := json.Marshal(map[string]string{"default_branch": target})
	status, resp, err := c.do(ctx, http.MethodPut, projectPath(ref), body)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("set default branch: HTTP %d: %s", status, snippet(resp))
	}
	return nil
}

func (c *Client) createBranch(ctx context.Context, ref core.RepoRef, newName, fromRef string) error {
	p := fmt.Sprintf("%s/repository/branches?branch=%s&ref=%s",
		projectPath(ref), url.QueryEscape(newName), url.QueryEscape(fromRef))
	status, resp, err := c.do(ctx, http.MethodPost, p, nil)
	if err != nil {
		return err
	}
	// GitLab returns 400 ("Branch already exists") when it's already there; the
	// caller reached here only after finding no open MR, so treat it as present.
	if status == http.StatusBadRequest || status == http.StatusConflict {
		return nil
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("create branch %s: HTTP %d: %s", newName, status, snippet(resp))
	}
	return nil
}

// ── FileRemediator ───────────────────────────────────────────────────────────

const fixBranch = "plumbline/dependency-automation"

// FileFixableChecks lists checks this connector can fix by opening an MR.
func (c *Client) FileFixableChecks() []string { return []string{"dependency-automation"} }

// OpenFix opens (or reuses) a merge request remediating check for ref.
func (c *Client) OpenFix(ctx context.Context, ref core.RepoRef, check string) (string, error) {
	switch check {
	case "dependency-automation":
		return c.openRenovateMR(ctx, ref)
	default:
		return "", fmt.Errorf("gitlab: cannot file-fix %q", check)
	}
}

func (c *Client) openRenovateMR(ctx context.Context, ref core.RepoRef) (string, error) {
	if ref.DefaultBranch == "" {
		return "", errors.New("gitlab: repo has no default branch")
	}
	if u, ok, err := c.existingMR(ctx, ref, fixBranch); err != nil {
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
	return c.createMR(ctx, ref, fixBranch, ref.DefaultBranch,
		"chore: add renovate.json",
		"Adds a Renovate config so dependency updates are automated.\n\nOpened by plumbline.")
}

func (c *Client) createFile(ctx context.Context, ref core.RepoRef, branch, path string, content []byte, message string) error {
	body, _ := json.Marshal(map[string]string{
		"branch":         branch,
		"content":        string(content), // GitLab stores content raw unless encoding=base64
		"commit_message": message,
	})
	enc := strings.ReplaceAll(url.PathEscape(path), "/", "%2F")
	p := fmt.Sprintf("%s/repository/files/%s", projectPath(ref), enc)
	status, resp, err := c.do(ctx, http.MethodPost, p, body)
	if err != nil {
		return err
	}
	if status == http.StatusBadRequest || status == http.StatusConflict {
		return nil // file already exists on the branch — proceed to the MR
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("create %s: HTTP %d: %s", path, status, snippet(resp))
	}
	return nil
}

func (c *Client) createMR(ctx context.Context, ref core.RepoRef, source, target, title, description string) (string, error) {
	body, _ := json.Marshal(map[string]string{
		"source_branch": source,
		"target_branch": target,
		"title":         title,
		"description":   description,
	})
	status, resp, err := c.do(ctx, http.MethodPost, projectPath(ref)+"/merge_requests", body)
	if err != nil {
		return "", err
	}
	if status == http.StatusConflict {
		if u, ok, e := c.existingMR(ctx, ref, source); e == nil && ok {
			return u, nil
		}
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("open MR: HTTP %d: %s", status, snippet(resp))
	}
	var mr struct {
		WebURL string `json:"web_url"`
	}
	if err := json.Unmarshal(resp, &mr); err != nil {
		return "", err
	}
	return mr.WebURL, nil
}

// existingMR finds an open MR whose source branch is sourceRef.
func (c *Client) existingMR(ctx context.Context, ref core.RepoRef, sourceRef string) (string, bool, error) {
	p := fmt.Sprintf("%s/merge_requests?state=opened&source_branch=%s",
		projectPath(ref), url.QueryEscape(sourceRef))
	status, body, err := c.get(ctx, p)
	if err != nil {
		return "", false, err
	}
	if status != http.StatusOK {
		return "", false, fmt.Errorf("list MRs: HTTP %d: %s", status, snippet(body))
	}
	var mrs []struct {
		WebURL string `json:"web_url"`
	}
	if err := json.Unmarshal(body, &mrs); err != nil {
		return "", false, err
	}
	if len(mrs) > 0 {
		return mrs[0].WebURL, true, nil
	}
	return "", false, nil
}
