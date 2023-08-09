package rest

import (
	"context"
	"path"
	"strconv"

	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/daemon"
	"github.com/akitasoftware/akita-libs/github"
)

type frontClientImpl struct {
	BaseClient
}

func NewFrontClient(host string, cli akid.ClientID) *frontClientImpl {
	return &frontClientImpl{
		BaseClient: NewBaseClient(host, cli),
	}
}

func (c *frontClientImpl) GetServices(ctx context.Context) ([]Service, error) {
	resp := []Service{}
	if err := c.Get(ctx, "/v1/services", &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *frontClientImpl) GetUser(ctx context.Context) (User, error) {
	resp := User{}
	err := c.Get(ctx, "/v1/user", &resp)
	return resp, err
}

func (c *frontClientImpl) DaemonHeartbeat(ctx context.Context, daemonName string) error {
	body := struct {
		DaemonName string `json:"daemon_name"`
	}{
		DaemonName: daemonName,
	}
	resp := struct{}{}
	return c.Post(ctx, "/v1/daemon/heartbeat", body, &resp)
}

func (c *frontClientImpl) LongPollActiveTracesForService(ctx context.Context, daemonName string, serviceID akid.ServiceID, activeTraces []akid.LearnSessionID) (daemon.ActiveTraceDiff, error) {
	body := struct {
		DaemonName     string                `json:"daemon_name"`
		ActiveTraceIDs []akid.LearnSessionID `json:"active_trace_ids"`
	}{
		DaemonName:     daemonName,
		ActiveTraceIDs: activeTraces,
	}
	var resp daemon.ActiveTraceDiff
	path := path.Join("/v1/services", akid.String(serviceID), "daemon")
	err := c.Post(ctx, path, body, &resp)
	return resp, err
}

func (c *frontClientImpl) GetGitHubPREnabledState(ctx context.Context, gitHubPR *github.PullRequest) (bool, error) {
	endpoint := path.Join("/v1/integrations/github/repos", gitHubPR.Repo.Owner, gitHubPR.Repo.Name, "prs", strconv.Itoa(gitHubPR.Num), "akita-enabled")
	response := struct {
		AkitaEnabled bool `json:"akita_enabled"`
	}{}
	if err := c.Get(ctx, endpoint, &response); err != nil {
		return false, err
	}
	return response.AkitaEnabled, nil
}

func (c *frontClientImpl) CreateService(ctx context.Context, serviceName, collectionId, env string) (CreateServiceResponse, error) {
	resp := CreateServiceResponse{}
	body := struct {
		Name            string          `json:"name"`
		PostmanMetaData PostmanMetaData `json:"postman_meta_data"`
	}{
		Name: serviceName,
		PostmanMetaData: PostmanMetaData{
			CollectionID: collectionId,
			Environment:  env,
		},
	}

	err := c.Post(ctx, "/v1/services", body, &resp)

	return resp, err
}
