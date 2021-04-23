package rest

import (
	"context"
	"path"

	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/daemon"
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

func (c *frontClientImpl) DaemonHeartbeat(ctx context.Context, daemonName string) error {
	body := struct {
		DaemonName string `json:"daemon_name"`
	}{
		DaemonName: daemonName,
	}
	resp := struct{}{}
	return c.post(ctx, "/v1/daemon/heartbeat", body, &resp)
}

func (c *frontClientImpl) LongPollActiveTracesForService(ctx context.Context, serviceID akid.ServiceID, activeTraces []akid.LearnSessionID) ([]daemon.LoggingOptions, error) {
	var resp []daemon.LoggingOptions
	path := path.Join("/v1/services", akid.String(serviceID), "daemon")
	err := c.get(ctx, path, &resp)
	return resp, err
}

func (c *frontClientImpl) LongPollForTraceDeactivation(ctx context.Context, serviceID akid.ServiceID, traceID akid.LearnSessionID) error {
	var resp struct{}
	path := path.Join("/v1/services", akid.String(serviceID), "learn", akid.String(traceID), "daemon")
	err := c.get(ctx, path, &resp)
	return err
}
