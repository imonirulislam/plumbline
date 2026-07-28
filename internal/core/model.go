// Package core holds plumbline's provider-agnostic domain: the normalized repo
// model, three-valued facts, policy config, and check results. Nothing here
// knows about GitHub, Gitea, or any specific API — adapters translate into it.
package core

// Tri is a three-valued fact produced by a provider adapter. A provider that
// cannot express a given fact returns TriUnsupported (never a failure), which
// checks surface as an "unsupported" verdict rather than a "fail".
type Tri int

const (
	TriUnknown     Tri = iota // not determined
	TriYes                    // fact is true
	TriNo                     // fact is false
	TriUnsupported            // the provider cannot express this fact
)

func (t Tri) String() string {
	switch t {
	case TriYes:
		return "yes"
	case TriNo:
		return "no"
	case TriUnsupported:
		return "unsupported"
	default:
		return "unknown"
	}
}

// Verdict is the outcome of a single check against a single repo.
type Verdict string

const (
	Pass        Verdict = "pass"
	Fail        Verdict = "fail"
	Unsupported Verdict = "unsupported" // provider can't express the fact
	Skip        Verdict = "skip"        // policy doesn't require this check
	Err         Verdict = "error"       // the check could not be evaluated
)

// RepoRef identifies a repository and carries the cheap attributes returned by
// a listing call.
type RepoRef struct {
	Owner         string `json:"owner"`
	Name          string `json:"name"`
	DefaultBranch string `json:"default_branch"`
	Private       bool   `json:"private"`
	Archived      bool   `json:"archived"`
	URL           string `json:"url"`
}

// Slug is the "owner/name" identifier.
func (r RepoRef) Slug() string { return r.Owner + "/" + r.Name }

// RepoState is the normalized set of facts a provider inspects for one repo.
// Checks read from this; they never call a provider directly.
type RepoState struct {
	Ref                     RepoRef
	DefaultBranchProtected  Tri
	HasCI                   Tri
	HasDependencyAutomation Tri
}

// CheckResult is one check's outcome for one repo.
type CheckResult struct {
	Check   string  `json:"check"`
	Verdict Verdict `json:"verdict"`
	Detail  string  `json:"detail,omitempty"`
}

// RepoReport is all check results for one repo.
type RepoReport struct {
	Repo     string        `json:"repo"`
	Archived bool          `json:"archived"`
	Results  []CheckResult `json:"results"`
}
