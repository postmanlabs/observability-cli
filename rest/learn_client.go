package rest

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"time"

	"github.com/akitasoftware/akita-libs/akid"
	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/path_trie"
	"github.com/akitasoftware/akita-libs/tags"
)

var (
	// Value that will be marshalled into an empty JSON object.
	emptyObject = map[string]interface{}{}
)

type learnClientImpl struct {
	baseClient

	serviceID akid.ServiceID
}

func NewLearnClient(host string, cli akid.ClientID, svc akid.ServiceID) *learnClientImpl {
	return &learnClientImpl{
		baseClient: newBaseClient(host, cli),
		serviceID:  svc,
	}
}

func (c *learnClientImpl) ListLearnSessions(ctx context.Context, svc akid.ServiceID, tags map[tags.Key]string) ([]*kgxapi.ListedLearnSession, error) {
	p := path.Join("/v1/services", akid.String(c.serviceID), "learn")
	q := url.Values{}
	for k, v := range tags {
		q.Add(fmt.Sprintf("tag[%s]", k), v)
	}

	var resp kgxapi.ListSessionsResponse
	err := c.getWithQuery(ctx, p, q, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Sessions, nil
}

func (c *learnClientImpl) ListLearnSessionsWithStats(ctx context.Context, svc akid.ServiceID, limit int) ([]*kgxapi.ListedLearnSession, error) {
	p := path.Join("/v1/services", akid.String(c.serviceID), "learn")
	q := url.Values{}
	q.Add("limit", fmt.Sprintf("%d", limit))
	q.Add("get_stats", "true")

	var resp kgxapi.ListSessionsResponse
	err := c.getWithQuery(ctx, p, q, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Sessions, nil
}

func (c *learnClientImpl) GetLearnSession(ctx context.Context, svc akid.ServiceID, lrn akid.LearnSessionID) (*kgxapi.LearnSession, error) {
	p := path.Join("/v1/services", akid.String(c.serviceID), "learn", akid.String(lrn))
	var resp kgxapi.LearnSession
	err := c.get(ctx, p, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *learnClientImpl) CreateLearnSession(ctx context.Context, baseSpecRef *kgxapi.APISpecReference, name string, tags map[tags.Key]string) (akid.LearnSessionID, error) {
	req := kgxapi.CreateLearnSessionRequest{BaseAPISpecRef: baseSpecRef, Tags: tags, Name: name}
	var resp kgxapi.LearnSession
	p := path.Join("/v1/services", akid.String(c.serviceID), "learn")
	err := c.post(ctx, p, req, &resp)
	if err != nil {
		return akid.LearnSessionID{}, err
	}
	return resp.ID, nil
}

func (c *learnClientImpl) ReportWitnesses(ctx context.Context, lrn akid.LearnSessionID, reports []*kgxapi.WitnessReport) error {
	req := kgxapi.UploadWitnessesRequest{
		ClientID: c.clientID,
		Reports:  reports,
	}
	resp := map[string]interface{}{}

	p := path.Join("/v1/services", akid.String(c.serviceID), "learn", akid.String(lrn), "async_witnesses")
	return c.post(ctx, p, req, &resp)
}

func (c *learnClientImpl) CreateSpec(ctx context.Context, name string, lrns []akid.LearnSessionID, opts CreateSpecOptions) (akid.APISpecID, error) {
	// Go cannot marshal regexp into JSON unfortunately.
	pathExclusions := make([]string, len(opts.PathExclusions))
	for i, e := range opts.PathExclusions {
		pathExclusions[i] = e.String()
	}

	req := map[string]interface{}{
		"name":              name,
		"learn_session_ids": lrns,
		"path_patterns":     opts.PathPatterns,
		"path_exclusions":   pathExclusions,
		"tags":              opts.Tags,
		"versions":          opts.Versions,
	}
	if opts.GitHubPR != nil {
		req["github_pr"] = opts.GitHubPR
	}
	if opts.GitLabMR != nil {
		req["gitlab_mr"] = opts.GitLabMR
	}
	if opts.TimeRange != nil {
		req["time_range"] = opts.TimeRange
	}

	p := path.Join("/v1/services", akid.String(c.serviceID), "specs")
	var resp kgxapi.CreateSpecResponse
	err := c.post(ctx, p, req, &resp)
	return resp.ID, err
}

func (c *learnClientImpl) GetSpec(ctx context.Context, api akid.APISpecID, opts GetSpecOptions) (kgxapi.GetSpecResponse, error) {
	qs := make(url.Values)
	if !opts.EnableRelatedTypes {
		qs.Add("strip_related_annotations", "true")
	}
	p := path.Join("/v1/services", akid.String(c.serviceID), "specs", akid.String(api))

	var resp kgxapi.GetSpecResponse
	err := c.getWithQuery(ctx, p, qs, &resp)
	return resp, err
}

func (c *learnClientImpl) ListSpecs(ctx context.Context) ([]kgxapi.SpecInfo, error) {
	qs := make(url.Values)

	// Set limit to 0 to ensure no pagination is applied.
	qs.Add("limit", "0")
	qs.Add("offset", "0")

	p := path.Join("/v1/services", akid.String(c.serviceID), "specs")

	var resp kgxapi.ListSpecsResponse
	err := c.getWithQuery(ctx, p, qs, &resp)
	return resp.Specs, err
}

func (c *learnClientImpl) GetSpecVersion(ctx context.Context, version string) (kgxapi.APISpecVersion, error) {
	var resp kgxapi.APISpecVersion
	p := path.Join("/v1/services", akid.String(c.serviceID), "spec-versions", version)
	err := c.get(ctx, p, &resp)
	if err != nil {
		return kgxapi.APISpecVersion{}, err
	}
	return resp, nil
}

func (c *learnClientImpl) UploadSpec(ctx context.Context, req kgxapi.UploadSpecRequest) (*kgxapi.UploadSpecResponse, error) {
	p := path.Join("/v1/services", akid.String(c.serviceID), "upload-spec")
	var resp kgxapi.UploadSpecResponse
	err := c.post(ctx, p, req, &resp)
	return &resp, err
}

func (c *learnClientImpl) GetAPISpecIDByName(ctx context.Context, n string) (akid.APISpecID, error) {
	resp := struct {
		ID akid.APISpecID `json:"id"`
	}{}
	path := fmt.Sprintf("/v1/services/%s/ids/specs/%s", akid.String(c.serviceID), n)
	err := c.get(ctx, path, &resp)
	return resp.ID, err
}

func (c *learnClientImpl) GetLearnSessionIDByName(ctx context.Context, n string) (akid.LearnSessionID, error) {
	resp := struct {
		ID akid.LearnSessionID `json:"id"`
	}{}
	path := fmt.Sprintf("/v1/services/%s/ids/learn_sessions/%s", akid.String(c.serviceID), n)
	err := c.get(ctx, path, &resp)
	return resp.ID, err
}

func (c *learnClientImpl) GetSpecDiffTrie(ctx context.Context, baseID, newID akid.APISpecID) (*path_trie.PathTrie, error) {
	var resp path_trie.PathTrie
	path := fmt.Sprintf("/v1/services/%s/specs/%s/diff/%s/trie",
		akid.String(c.serviceID), akid.String(baseID), akid.String(newID))
	err := c.get(ctx, path, &resp)
	return &resp, err
}

func (c *learnClientImpl) SetSpecVersion(ctx context.Context, specID akid.APISpecID, versionName string) error {
	resp := struct {
	}{}
	path := fmt.Sprintf("/v1/services/%s/spec-versions/%s",
		akid.String(c.serviceID), versionName)
	req := kgxapi.SetSpecVersionRequest{
		APISpecID: specID,
	}

	return c.post(ctx, path, req, &resp)
}

// Returns individual events
func (c *learnClientImpl) GetUnaggregatedTimeline(ctx context.Context, serviceID akid.ServiceID, deployment string, start time.Time, end time.Time, limit int) (kgxapi.TimelineResponse, error) {
	path := fmt.Sprintf("/v1/services/%s/timeline/%s/query",
		akid.String(serviceID), deployment)
	q := url.Values{}
	q.Add("start", fmt.Sprintf("%d", start.Unix()*1000000))
	q.Add("end", fmt.Sprintf("%d", end.Unix()*1000000))
	q.Add("limit", fmt.Sprintf("%d", limit))
	// Separate out by response code
	q.Add("key", "host")
	q.Add("key", "method")
	q.Add("key", "path")
	q.Add("key", "code")

	var resp kgxapi.TimelineResponse
	err := c.getWithQuery(ctx, path, q, &resp)
	return resp, err
}
