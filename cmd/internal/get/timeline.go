package get

import (
	"container/heap"
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/api_schema"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var GetTimelineCmd = &cobra.Command{
	Use:          "timeline [SERVICE] [DEPLOYMENT]",
	Aliases:      []string{"timeline"},
	Short:        "List timeline for the given project.",
	Long:         "List timeline of API calls for the given project.",
	SilenceUsage: false,
	RunE:         getTimeline,
}

var (
	deploymentFlag    string
	startTimeFlag     string
	endTimeFlag       string
	timelineLimitFlag int
)

func init() {
	Cmd.AddCommand(GetTimelineCmd)

	GetTimelineCmd.Flags().StringVar(
		&serviceFlag,
		"project",
		"",
		"Your Akita project.")

	GetTimelineCmd.Flags().StringVar(
		&serviceFlag,
		"service",
		"",
		"Your Akita project. DEPRECATED, prefer --project.")

	GetTimelineCmd.Flags().StringVar(
		&serviceFlag,
		"cluster",
		"",
		"Your Akita project. DEPRECATED, prefer --project.")

	GetTimelineCmd.Flags().StringVar(
		&deploymentFlag,
		"deployment",
		"",
		"Deployment tag used for traces. DEPRECATED.")

	GetTimelineCmd.Flags().StringVar(
		&startTimeFlag,
		"start",
		"",
		"Time start (default 1 week ago). Must be given in RFC3339 format, as YYYY-MM-DDTHH:MM:SS+00:00")

	GetTimelineCmd.Flags().StringVar(
		&endTimeFlag,
		"end",
		"",
		"Time end (default now), must be RFC3339 format")

	GetTimelineCmd.Flags().IntVar(
		&timelineLimitFlag,
		"limit",
		100,
		"Show N time points.")
}

// Implements Heap interface
type TimelineHeap []*api_schema.Timeline

var _ heap.Interface = (*TimelineHeap)(nil)

func (h TimelineHeap) Len() int {
	return len(h)
}

func (h TimelineHeap) Less(i, j int) bool {
	// Empty timelines go first, then find the one with the earliest remaining event
	return len(h[i].Events) == 0 ||
		(len(h[j].Events) != 0 &&
			h[i].Events[0].Time.Before(h[j].Events[0].Time))
}

func (h TimelineHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *TimelineHeap) Push(x interface{}) {
	*h = append(*h, x.(*api_schema.Timeline))
}

func (h *TimelineHeap) Pop() interface{} {
	old := *h
	n := len(old)
	*h = old[0 : n-1]
	return old[n-1]
}

func getTimeline(cmd *cobra.Command, args []string) error {
	// Accept these as either flags or arguments.
	if serviceFlag == "" {
		if len(args) == 0 {
			return errors.New("Must specify a project")
		}
		serviceFlag = args[0]
		args = args[1:]
	}
	if deploymentFlag == "" {
		if len(args) == 0 {
			deploymentFlag = "default"
		} else {
			deploymentFlag = args[0]
		}
		args = args[1:]
	}

	if len(args) > 0 {
		return errors.New("Too many command line parameters.")
	}

	end := time.Now()
	start := end.Add(-7 * 24 * time.Hour)
	var err error
	// TODO: more variations of start time?
	if startTimeFlag != "" {
		start, err = time.Parse(time.RFC3339, startTimeFlag)
		if err != nil {
			return errors.Wrapf(err, "Couldn't parse start time.")
		}
	}

	if endTimeFlag != "" {
		end, err = time.Parse(time.RFC3339, endTimeFlag)
		if err != nil {
			return errors.Wrapf(err, "Couldn't parse end time.")
		}
	}

	printer.Debugf("Loading project %q deployment %q from %v to %v\n", serviceFlag, deploymentFlag, start, end)

	clientID := akid.GenerateClientID()
	frontClient := rest.NewFrontClient(rest.Domain, clientID)
	serviceID, err := util.GetServiceIDByName(frontClient, serviceFlag)
	if err != nil {
		return cmderr.AkitaErr{Err: err}
	}

	learnClient := rest.NewLearnClient(rest.Domain, clientID, serviceID)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	resp, err := learnClient.GetTimeline(ctx, serviceID, deploymentFlag, start, end, timelineLimitFlag)
	if err != nil {
		return cmderr.AkitaErr{Err: err}
	}
	h := TimelineHeap(make([]*api_schema.Timeline, len(resp.Timelines)))
	for i := range resp.Timelines {
		h[i] = &resp.Timelines[i]
	}

	heap.Init(&h)

	for h.Len() > 0 {
		timeline := h[0]
		event := timeline.Events[0]
		fmt.Printf("%s %9.3fms %6s %s %s %s\n",
			event.Time.Format(time.RFC3339),
			float32Value(event.Values.P99Latency),
			formatStringAttr(timeline.GroupAttributes.Method.String()),
			formatStringAttr(timeline.GroupAttributes.Host.String()),
			formatStringAttr(timeline.GroupAttributes.PathTemplate.String()),
			formatIntAttr(timeline.GroupAttributes.ResponseCode))
		timeline.Events = timeline.Events[1:]
		if len(timeline.Events) == 0 {
			heap.Pop(&h)
		} else {
			heap.Fix(&h, 0)
		}
	}

	return nil
}

func float32Value(val *float32) float32 {
	if val == nil {
		return 0
	}
	return *val
}

func formatStringAttr(val string) string {
	if val == "" {
		return "*"
	}
	return val
}

func formatIntAttr(val int) string {
	if val == 0 {
		return "*"
	}
	return strconv.Itoa(val)
}
