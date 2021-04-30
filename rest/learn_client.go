package rest

import (
	"context"
	"fmt"
	"net/url"
	"path"

	"github.com/akitasoftware/akita-libs/akid"
	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/daemon"
	"github.com/akitasoftware/akita-libs/path_trie"
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

func (c *learnClientImpl) ListLearnSessions(ctx context.Context, svc akid.ServiceID, tags map[string]string) ([]*kgxapi.LearnSession, error) {
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

func (c *learnClientImpl) GetLearnSession(ctx context.Context, svc akid.ServiceID, lrn akid.LearnSessionID) (*kgxapi.LearnSession, error) {
	p := path.Join("/v1/services", akid.String(c.serviceID), "learn", akid.String(lrn))
	var resp kgxapi.LearnSession
	err := c.get(ctx, p, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *learnClientImpl) CreateLearnSession(ctx context.Context, baseSpecRef *kgxapi.APISpecReference, name string, tags map[string]string) (akid.LearnSessionID, error) {
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

func (c *learnClientImpl) CheckpointLearnSession(ctx context.Context, lrn akid.LearnSessionID) (akid.APISpecID, error) {
	var resp kgxapi.CheckpointResponse
	p := path.Join("/v1/services", akid.String(c.serviceID), "learn", akid.String(lrn), "async_checkpoint")
	err := c.post(ctx, p, emptyObject, &resp)
	if err != nil {
		return akid.APISpecID{}, err
	}
	return resp.APISpecID, nil
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
	}
	if opts.GitHubPR != nil {
		req["github_pr"] = opts.GitHubPR
	}
	if opts.GitLabMR != nil {
		req["gitlab_mr"] = opts.GitLabMR
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

func (c *learnClientImpl) LongPollServiceLoggingStatus(ctx context.Context, serviceID akid.ServiceID, currentlyLogging bool) (*daemon.LoggingState, error) {
	var resp daemon.LoggingState
	path := fmt.Sprintf("/v1/services/%s/daemon", akid.String(c.serviceID))
	err := c.get(ctx, path, &resp)
	return &resp, err
}
