package nginx

import (
	"context"
	"sync"
	"time"

	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-cli/architecture"
	"github.com/akitasoftware/akita-cli/env"
	"github.com/akitasoftware/akita-cli/plugin"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/akitasoftware/akita-cli/trace"
	"github.com/akitasoftware/akita-cli/usage"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-cli/version"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/api_schema"
	kgxapi "github.com/akitasoftware/akita-libs/api_schema"
	"github.com/akitasoftware/akita-libs/tags"
	"github.com/akitasoftware/go-utils/optionals"
)

const (
	// Context timeout for telemetry upload
	telemetryTimeout = 30 * time.Second
)

type NginxBackend struct {
	args *Args

	backendSvc  akid.ServiceID
	deployment  string
	learnClient rest.LearnClient
	collector   trace.Collector
	summary     *trace.PacketCounter

	showSuccess sync.Once
	startTime   time.Time
}

type Args struct {
	Domain      string
	ClientID    akid.ClientID
	ServiceName string
	ListenPort  uint16

	MaxWitnessSize_bytes int
	Plugins              []plugin.AkitaPlugin
	StatsLogDelay        int
	TelemetryInterval    int
}

// Send the initial message to the backend indicating successful start
// TODO: refactor to share this telemetry infrastructure with apidump?
func (b *NginxBackend) SendInitialTelemetry() {
	req := kgxapi.PostInitialClientTelemetryRequest{
		ClientID:                  b.args.ClientID,
		ObservedStartingAt:        b.startTime,
		ObservedDurationInSeconds: b.args.StatsLogDelay,
		CLIVersion:                version.ReleaseVersion().String(),
		CLITargetArch:             architecture.GetCanonicalArch(),
		AkitaDockerRelease:        env.InDocker(),
		DockerDesktop:             false,
	}
	ctx, cancel := context.WithTimeout(context.Background(), telemetryTimeout)
	defer cancel()
	err := b.learnClient.PostInitialClientTelemetry(ctx, b.backendSvc, b.deployment, req)
	if err != nil {
		// Log an error and continue.
		printer.Stderr.Errorf("Failed to send initial telemetry statistics: %s\n", err)
		telemetry.Error("telemetry", err)
	}
}

// Fill in the client ID and start time and send telemetry to the backend.
func (b *NginxBackend) SendTelemetry(req *kgxapi.PostClientPacketCaptureStatsRequest) {
	req.ClientID = b.args.ClientID
	req.ObservedStartingAt = b.startTime

	ctx, cancel := context.WithTimeout(context.Background(), telemetryTimeout)
	defer cancel()
	err := b.learnClient.PostClientPacketCaptureStats(ctx, b.backendSvc, b.deployment, *req)
	if err != nil {
		// Log an error and continue.
		printer.Stderr.Errorf("Failed to send telemetry statistics: %s\n", err)
		telemetry.Error("telemetry", err)
	}
}

// Send a message to the backend indicating failure to start and a cause
func (b *NginxBackend) SendErrorTelemetry(errorType api_schema.ApidumpErrorType, err error) {
	req := &kgxapi.PostClientPacketCaptureStatsRequest{
		ObservedDurationInSeconds: b.args.StatsLogDelay,
		ApidumpError:              errorType,
		ApidumpErrorText:          err.Error(),
	}
	b.SendTelemetry(req)
}

// Update the backend with new current capture stats.
func (b *NginxBackend) SendPacketTelemetry(observedDuration int) {
	req := &kgxapi.PostClientPacketCaptureStatsRequest{
		AgentResourceUsage:        usage.Get(),
		ObservedDurationInSeconds: observedDuration,
	}
	if b.summary != nil {
		req.PacketCountSummary = b.summary.Summary(2)
	}

	b.SendTelemetry(req)
}

// Goroutine for sending telemetry.
// Show on the console after StatsLogDelay seconds, then send packet telemetry.
func (b *NginxBackend) TelemetryWorker(done <-chan struct{}) {
	if b.args.StatsLogDelay > 0 {
		// Wait while capturing statistics.
		time.Sleep(time.Duration(b.args.StatsLogDelay) * time.Second)

		// Print telemetry data (reduced compared to apidump)
		total := b.summary.TotalOnPort(fakeNginxPort)
		printer.Stderr.Infof("%d requests and %d responses seen after %d seconds of capture.\n",
			total.HTTPRequests,
			total.HTTPResponses,
			b.args.StatsLogDelay)
		b.SendPacketTelemetry(b.args.StatsLogDelay)
	}

	if b.args.TelemetryInterval > 0 {
		ticker := time.NewTicker(time.Duration(b.args.TelemetryInterval) * time.Second)

		for {
			select {
			case <-done:
				return
			case now := <-ticker.C:
				duration := int(now.Sub(b.startTime) / time.Second)
				b.SendPacketTelemetry(duration)
			}
		}
	}
}

// Log the first successful request
func (b *NginxBackend) ReportSuccess(host string) {
	b.showSuccess.Do(func() {
		printer.Stderr.Infof("Successfully received first mirrored request from NGINX for host %q\n", host)
	})
}

func NewNginxBackend(args *Args) (*NginxBackend, error) {
	b := &NginxBackend{
		args:       args,
		deployment: "default",
		startTime:  time.Now(),
	}

	frontClient := rest.NewFrontClient(args.Domain, args.ClientID)
	backendSvc, err := util.GetServiceIDByName(frontClient, args.ServiceName)
	if err != nil {
		return nil, err
	}
	b.backendSvc = backendSvc
	b.learnClient = rest.NewLearnClient(args.Domain, args.ClientID, backendSvc)

	traceTags := map[tags.Key]string{}
	traceName := util.RandomLearnSessionName()
	backendLrn, err := util.NewLearnSession(args.Domain, args.ClientID, b.backendSvc, traceName, traceTags, nil)
	if err != nil {
		return nil, errors.Wrap(err, "failed to create trace or fetch existing trace")
	}
	printer.Infof("Created new trace on Akita Cloud: %s\n", traceName)

	b.summary = trace.NewPacketCounter()
	b.collector = trace.NewBackendCollector(b.backendSvc, backendLrn, b.learnClient,
		optionals.Some(args.MaxWitnessSize_bytes), b.summary, args.Plugins)

	// TODO: rate-limit
	// TODO: session rotation
	// TODO: filters?

	// Count the requests and responses
	b.collector = &trace.PacketCountCollector{
		PacketCounts: b.summary,
		Collector:    b.collector,
	}

	return b, nil
}
