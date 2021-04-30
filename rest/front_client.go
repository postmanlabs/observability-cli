package rest

import (
	"context"

	"github.com/akitasoftware/akita-libs/akid"
)

type frontClientImpl struct {
	baseClient
}

func NewFrontClient(host string, cli akid.ClientID) *frontClientImpl {
	return &frontClientImpl{
		baseClient: newBaseClient(host, cli),
	}
}

func (c *frontClientImpl) GetServices(ctx context.Context) ([]Service, error) {
	resp := []Service{}
	if err := c.get(ctx, "/v1/services", &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *frontClientImpl) DaemonHeartbeat(ctx context.Context) error {
	resp := struct{}{}
	return c.get(ctx, "/v1/daemon/heartbeat", &resp)
}
