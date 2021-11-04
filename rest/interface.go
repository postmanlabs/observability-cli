package rest

import (
	"context"
	"regexp"

	"github.com/akitasoftware/akita-libs/akid"
	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/daemon"
	"github.com/akitasoftware/akita-libs/github"
	"github.com/akitasoftware/akita-libs/gitlab"
	pp "github.com/akitasoftware/akita-libs/path_pattern"
	"github.com/akitasoftware/akita-libs/path_trie"
	"github.com/akitasoftware/akita-libs/tags"
	"github.com/akitasoftware/akita-libs/time_span"
)

type GetSpecOptions struct {
	EnableRelatedTypes bool
}

type CreateSpecOptions struct {
	Tags           map[tags.Key]string
	Versions       []string
	PathPatterns   []pp.Pattern
	PathExclusions []*regexp.Regexp
	GitHubPR       *github.PRInfo
	GitLabMR       *gitlab.MRInfo
	TimeRange      *time_span.TimeSpan
}

type LearnClient interface {
	ListLearnSessions(context.Context, akid.ServiceID, map[tags.Key]string) ([]*kgxapi.ListedLearnSession, error)
	ListLearnSessionsWithStats(context.Context, akid.ServiceID, int) ([]*kgxapi.ListedLearnSession, error)
	GetLearnSession(context.Context, akid.ServiceID, akid.LearnSessionID) (*kgxapi.LearnSession, error)
	CreateLearnSession(context.Context, *kgxapi.APISpecReference, string, map[tags.Key]string) (akid.LearnSessionID, error)

	// Uploads a batch of reports to Akita Cloud. This method is responsible for
	// filling in the ClientID in the given ReportsUploadRequest.
	AsyncReportsUpload(context.Context, akid.LearnSessionID, *kgxapi.UploadReportsRequest) error

	// Creates a spec from a set of learn sessions.
	CreateSpec(context.Context, string, []akid.LearnSessionID, CreateSpecOptions) (akid.APISpecID, error)
	GetSpec(context.Context, akid.APISpecID, GetSpecOptions) (kgxapi.GetSpecResponse, error)
	GetSpecVersion(context.Context, string) (kgxapi.APISpecVersion, error)
	UploadSpec(context.Context, kgxapi.UploadSpecRequest) (*kgxapi.UploadSpecResponse, error)

	// Resolve names.
	GetAPISpecIDByName(context.Context, string) (akid.APISpecID, error)
	GetLearnSessionIDByName(context.Context, string) (akid.LearnSessionID, error)

	// Spec diff
	GetSpecDiffTrie(context.Context, akid.APISpecID, akid.APISpecID) (*path_trie.PathTrie, error)
}

type FrontClient interface {
	GetServices(context.Context) ([]Service, error)
	DaemonHeartbeat(ctx context.Context, daemonName string) error

	// Long-polls for changes to the set of active traces for a service.
	// Callers specify what they think the current set of active traces is. When
	// the cloud has a different set, this method returns options for capturing
	// new traces and a set of deactivated traces. An error is returned if the
	// connection is dropped (e.g., due to timing out).
	LongPollActiveTracesForService(context context.Context, daemonName string, serviceID akid.ServiceID, currentTraces []akid.LearnSessionID) (daemon.ActiveTraceDiff, error)
}
