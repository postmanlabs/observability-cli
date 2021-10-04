package get

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/akitasoftware/akita-cli/cmd/internal/akiflag"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/api_schema"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var GetGraphCmd = &cobra.Command{
	Use:          "graph [SERVICE] [DEPLOYMENT]",
	Aliases:      []string{"graph"},
	Short:        "Show the service graph in textual form.",
	Long:         "Show the service graph in textual form.",
	SilenceUsage: false,
	RunE:         getGraph,
}

var (
	graphOutputFlag string
	graphTypeFlag   string
	hideUnknownFlag bool
)

func init() {
	Cmd.AddCommand(GetGraphCmd)

	GetGraphCmd.Flags().StringVar(
		&serviceFlag,
		"service",
		"",
		"Your Akita service.")

	GetGraphCmd.Flags().StringVar(
		&deploymentFlag,
		"deployment",
		"",
		"Deployment tag used for traces.")

	GetGraphCmd.Flags().StringVar(
		&startTimeFlag,
		"start",
		"",
		"Time start (default 1 week ago). Must be given in RFC3339 format, as YYYY-MM-DDTHH:MM:SS+00:00")

	GetGraphCmd.Flags().StringVar(
		&endTimeFlag,
		"end",
		"",
		"Time end (default now), must be RFC3339 format")

	GetGraphCmd.Flags().StringVar(
		&graphOutputFlag,
		"output",
		"source",
		"Output format: source (grouped by source), target (grouped by target), dot")

	GetGraphCmd.Flags().StringVar(
		&graphTypeFlag,
		"vertices",
		"services",
		"Graph target vertices: services, endpoints")

	GetGraphCmd.Flags().BoolVar(
		&hideUnknownFlag,
		"hide-unknown",
		false,
		"Exclude unknown vertices from the output",
	)
}

func getGraph(cmd *cobra.Command, args []string) error {
	// Accept these as either flags or arguments.
	if serviceFlag == "" {
		if len(args) == 0 {
			return errors.New("Must specify a service name.")
		}
		serviceFlag = args[0]
		args = args[1:]
	}
	if deploymentFlag == "" {
		if len(args) == 0 {
			return errors.New("Must specify a deployment name.")
		}
		deploymentFlag = args[0]
		args = args[1:]
	}

	if len(args) > 0 {
		return errors.New("Too many command line arguments.")
	}

	end := time.Now()
	start := end.Add(-7 * 24 * time.Hour)
	var err error

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

	var graphType string
	switch graphTypeFlag {
	case "services":
		graphType = "ServiceToService"
	case "endpoints":
		graphType = "ServiceToEndpoint"
	default:
		return errors.New("Unsupported graph type.")
	}

	var outputFn func(*api_schema.GraphResponse)
	switch graphOutputFlag {
	case "source":
		outputFn = printGraphBySource
	case "target":
		outputFn = printGraphByTarget
	case "dot", "graphviz":
		outputFn = printDot
	default:
		return errors.New("Unsupported output format.")
	}

	printer.Debugf("Loading service %q deployment %q from %v to %v\n", serviceFlag, deploymentFlag, start, end)

	clientID := akid.GenerateClientID()
	frontClient := rest.NewFrontClient(akiflag.Domain, clientID)
	serviceID, err := util.GetServiceIDByName(frontClient, serviceFlag)
	if err != nil {
		return cmderr.AkitaErr{Err: err}
	}

	learnClient := rest.NewLearnClient(akiflag.Domain, clientID, serviceID)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()
	resp, err := learnClient.GetGraphEdges(ctx, serviceID, deploymentFlag, start, end, graphType)
	if err != nil {
		return cmderr.AkitaErr{Err: err}
	}

	if hideUnknownFlag {
		replacementEdges := make([]api_schema.GraphEdge, 0, len(resp.Edges))
		for _, e := range resp.Edges {
			if e.SourceAttributes.Host != "" {
				replacementEdges = append(replacementEdges, e)
			}
		}
		resp.Edges = replacementEdges
	}

	outputFn(&resp)
	return nil
}

func endpointLessThan(a1 api_schema.EndpointGroupAttributes, a2 api_schema.EndpointGroupAttributes) bool {
	if a1.Host < a2.Host {
		return true
	}
	if a1.Host > a2.Host {
		return false
	}
	if a1.PathTemplate < a2.PathTemplate {
		return true
	}
	if a1.PathTemplate > a2.PathTemplate {
		return false
	}
	if a1.Method < a2.Method {
		return true
	}
	// Ignoring response code
	return false
}

func hostOrUnknown(h string) string {
	if h == "" {
		return "[unknown]"
	}
	return h
}

func printGraphBySource(graph *api_schema.GraphResponse) {
	sort.Slice(graph.Edges, func(i, j int) bool {
		return endpointLessThan(graph.Edges[i].SourceAttributes, graph.Edges[j].SourceAttributes) ||
			(graph.Edges[i].SourceAttributes == graph.Edges[j].SourceAttributes &&
				endpointLessThan(graph.Edges[i].TargetAttributes, graph.Edges[j].TargetAttributes))
	})

	for i, e := range graph.Edges {
		// TODO: this assumes service is the only supported source vertex, which is true right now.
		if i > 0 && e.SourceAttributes != graph.Edges[i-1].SourceAttributes {
			fmt.Printf("\n%-30s -> ", hostOrUnknown(e.SourceAttributes.Host))
		} else if i == 0 {
			fmt.Printf("%-30s -> ", hostOrUnknown(e.SourceAttributes.Host))
		} else {
			// Don't repeat source information
			fmt.Printf("%-30s -> ", "")
		}

		if e.TargetAttributes.PathTemplate == "" {
			fmt.Printf("%-30s\n", hostOrUnknown(e.TargetAttributes.Host))
		} else {
			fmt.Printf("%-30s %7s %s\n", e.TargetAttributes.Host, e.TargetAttributes.Method, e.TargetAttributes.PathTemplate)
		}
	}

}

func printGraphByTarget(graph *api_schema.GraphResponse) {
	sort.Slice(graph.Edges, func(i, j int) bool {
		return endpointLessThan(graph.Edges[i].TargetAttributes, graph.Edges[j].TargetAttributes) ||
			(graph.Edges[i].TargetAttributes == graph.Edges[j].TargetAttributes &&
				endpointLessThan(graph.Edges[i].SourceAttributes, graph.Edges[j].SourceAttributes))
	})

	for i, e := range graph.Edges {
		if i > 0 && e.TargetAttributes != graph.Edges[i-1].TargetAttributes {
			fmt.Printf("\n")
		}

		// TODO: this assumes service is the only supported source vertex, which is true right now.
		fmt.Printf("%-30s -> ", hostOrUnknown(e.SourceAttributes.Host))

		if (i > 0 && e.TargetAttributes != graph.Edges[i-1].TargetAttributes) || i == 0 {
			if e.TargetAttributes.PathTemplate == "" {
				fmt.Printf("%-30s\n", hostOrUnknown(e.TargetAttributes.Host))
			} else {
				fmt.Printf("%-30s %7s %s\n", e.TargetAttributes.Host, e.TargetAttributes.Method, e.TargetAttributes.PathTemplate)
			}
		} else {
			fmt.Printf("\n")
		}
	}
}

func printDot(graph *api_schema.GraphResponse) {
	fmt.Printf("digraph G {\n")
	for _, e := range graph.Edges {
		if e.TargetAttributes.PathTemplate == "" {
			fmt.Printf("  %q -> %q [label=\"%v\"]\n",
				hostOrUnknown(e.SourceAttributes.Host),
				e.TargetAttributes.Host,
				e.Values[api_schema.Event_Count])
		} else {
			fmt.Printf("  %q -> \"%s\\n%s %s\" [label=\"%v\"]\n",
				hostOrUnknown(e.SourceAttributes.Host),
				e.TargetAttributes.Host,
				e.TargetAttributes.Method,
				e.TargetAttributes.PathTemplate,
				e.Values[api_schema.Event_Count])
		}
	}
	fmt.Printf("}\n")
}
