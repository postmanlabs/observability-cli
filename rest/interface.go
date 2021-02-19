package rest

import (
	"context"
	"regexp"

	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/github"
	"github.com/akitasoftware/akita-libs/gitlab"
	pp "github.com/akitasoftware/akita-libs/path_pattern"
	"github.com/akitasoftware/akita-libs/path_trie"
)

type GetSpecOptions struct {
	EnableRelatedTypes bool
}

type CreateSpecOptions struct {
	Tags           map[string]string
	PathPatterns   []pp.Pattern
	PathExclusions []*regexp.Regexp
	GitHubPR       *github.PRInfo
	GitLabMR       *gitlab.MRInfo
}

type LearnClient interface {
	ListLearnSessions(context.Context, akid.ServiceID, map[string]string) ([]*kgxapi.LearnSession, error)
	GetLearnSession(context.Context, akid.ServiceID, akid.LearnSessionID) (*kgxapi.LearnSession, error)
	CreateLearnSession(context.Context, *kgxapi.APISpecReference, string, map[string]string) (akid.LearnSessionID, error)
	ReportWitnesses(context.Context, akid.LearnSessionID, []*kgxapi.WitnessReport) error

	// Deprecated: old way of creating a spec from a single learn session.
	// Use CreateSpec instead.
	CheckpointLearnSession(context.Context, akid.LearnSessionID) (akid.APISpecID, error)

	// Creates a spec from a set of learn sessions.
	CreateSpec(context.Context, string, []akid.LearnSessionID, CreateSpecOptions) (akid.APISpecID, error)
	GetSpec(context.Context, akid.APISpecID, GetSpecOptions) (kgxapi.GetSpecResponse, error)
	GetSpecVersion(context.Context, string) (kgxapi.APISpecVersion, error)

	// Resolve names.
	GetAPISpecIDByName(context.Context, string) (akid.APISpecID, error)
	GetLearnSessionIDByName(context.Context, string) (akid.LearnSessionID, error)

	// Spec diff
	GetSpecDiffTrie(context.Context, akid.APISpecID, akid.APISpecID) (*path_trie.PathTrie, error)
}

type FrontClient interface {
	GetServices(context.Context) ([]Service, error)
}
