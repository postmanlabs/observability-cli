package ecs

import (
	"fmt"

	ecs_cloudformation_utils "github.com/akitasoftware/akita-cli/aws_utils/cloudformation/ecs"
	ecs_console_utils "github.com/akitasoftware/akita-cli/aws_utils/console/ecs"
	"github.com/akitasoftware/akita-cli/cmd/internal/cmderr"
	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/go-utils/optionals"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var (
	// Postman Insights project id
	projectId string

	// For use when injecting a sidecar container into ECS. These will be
	// interactively prompted if not given on the command line. To inject into ECS
	// non-interactively, these must all be given.
	awsProfileFlag        string
	awsRegionFlag         string
	ecsClusterFlag        string
	ecsServiceFlag        string
	ecsTaskDefinitionFlag string

	// Location of credentials file.
	awsCredentialsFlag string

	// Output in YAML instead of JSON.
	yamlFlag bool

	// Print out the steps that would be taken, but do not do them
	dryRunFlag bool
)

var Cmd = &cobra.Command{
	Use:   "ecs",
	Short: "Add the Postman Insights Agent to AWS ECS.",
	Long:  "The agent will collect information from you and add the Postman Insights Agent container to an ECS Task.",
	// N.B.: this is useless because the root command makes its own determination,
	// need to return AkitaErr to not show the usage.
	SilenceUsage: true,
	RunE:         addAgentToECS,
}

// 'postman-insights-agent ecs' should default to 'postman-insights-agent ecs add'
var AddToECSCmd = &cobra.Command{
	Use:          "add",
	Short:        Cmd.Short,
	Long:         Cmd.Long,
	SilenceUsage: true,
	RunE:         addAgentToECS,
}

var RemoveFromECSCmd = &cobra.Command{
	Use:          "remove",
	Short:        "Remove the Postman Insights Agent from AWS ECS.",
	Long:         "Remove a previously installed Postman Insights Agent container from an ECS Task.",
	SilenceUsage: true,
	RunE:         removeAgentFromECS,

	// Temporarily hide from users until complete
	Hidden: true,
}

var PrintCloudFormationFragmentCmd = &cobra.Command{
	Use:   "cf-fragment",
	Short: "Print an AWS CloudFormation fragment for adding the Postman Insights Agent to AWS ECS.",
	Long:  "Print a code fragment that can be inserted into a CloudFormation template to add the Postman Insights Agent as a sidecar container.",
	RunE:  printCloudFormationFragment,
}

var PrintECSTaskDefinitionCmd = &cobra.Command{
	Use:   "task-def",
	Short: "Print an AWS ECS task definition for running the Postman Insights Agent in daemon mode.",
	Long:  "Print a task definition that can be added to an ECS cluster to run the Postman Insights Agent as a daemon in host-networking mode on every EC2 instance in the cluster.",
	RunE:  printECSTaskDefinition,
}

func init() {
	// TODO: add the ability to specify the credentials directly instead of via an AWS profile?
	Cmd.PersistentFlags().StringVar(&projectId, "project", "", "Your Insights Project ID")
	Cmd.PersistentFlags().StringVar(&awsProfileFlag, "profile", "", "Which of your AWS profiles to use to access ECS.")
	Cmd.PersistentFlags().StringVar(&awsRegionFlag, "region", "", "The AWS region in which your ECS cluster resides.")
	Cmd.PersistentFlags().StringVar(&ecsClusterFlag, "cluster", "", "The name or ARN of your ECS cluster.")
	Cmd.PersistentFlags().StringVar(&ecsServiceFlag, "service", "", "The name or ARN of your ECS service.")
	Cmd.PersistentFlags().StringVar(
		&ecsTaskDefinitionFlag,
		"task",
		"",
		"The name of your ECS task definition to modify.",
	)
	Cmd.PersistentFlags().BoolVar(
		&dryRunFlag,
		"dry-run",
		false,
		"Perform a dry run: show what will be done, but do not modify ECS.",
	)

	PrintCloudFormationFragmentCmd.Flags().BoolVar(
		&yamlFlag,
		"yaml",
		false,
		"Output as YAML instead of JSON",
	)

	// Support for credentials in a nonstandard location
	Cmd.PersistentFlags().StringVar(&awsCredentialsFlag, "aws-credentials", "", "Location of AWS credentials file.")
	Cmd.PersistentFlags().MarkHidden("aws-credentials")

	Cmd.AddCommand(AddToECSCmd)
	Cmd.AddCommand(PrintCloudFormationFragmentCmd)
	Cmd.AddCommand(PrintECSTaskDefinitionCmd)
	Cmd.AddCommand(RemoveFromECSCmd)
}

// Checks that an API key and a project ID are provided, and that the API key is
// valid for the project ID.
func checkAPIKeyAndProjectID() error {
	// Check for API key.
	_, err := cmderr.RequirePostmanAPICredentials("The Postman Insights Agent must have an API key in order to capture traces.")
	if err != nil {
		return err
	}

	// Check that a collection or project is provided.
	if projectId == "" {
		return errors.New("--project must be specified")
	}

	frontClient := rest.NewFrontClient(rest.Domain, telemetry.GetClientID())
	var serviceID akid.ServiceID
	err = akid.ParseIDAs(projectId, &serviceID)
	if err != nil {
		return errors.Wrap(err, "failed to parse service ID")
	}

	_, err = util.GetServiceNameByServiceID(frontClient, serviceID)
	if err != nil {
		return err
	}

	return nil
}

func addAgentToECS(cmd *cobra.Command, args []string) error {
	err := checkAPIKeyAndProjectID()
	if err != nil {
		return err
	}

	return RunAddWorkflow()
}

func removeAgentFromECS(cmd *cobra.Command, args []string) error {
	return fmt.Errorf("this command is not yet implemented")
}

func printCloudFormationFragment(cmd *cobra.Command, args []string) error {
	err := checkAPIKeyAndProjectID()
	if err != nil {
		return err
	}

	const isEssential = false
	agentContainer := makeAgentContainerDefinition(
		optionals.None[string](),
		optionals.None[string](),
		optionals.None[string](),
		isEssential,
	)

	formatter := ecs_cloudformation_utils.ContainerDefinitionToJSONForCloudFormation
	if yamlFlag {
		formatter = ecs_cloudformation_utils.ContainerDefinitionToYAMLForCloudFormation
	}
	result, err := formatter(agentContainer)
	if err != nil {
		return errors.Wrapf(err, "unable to format CloudFormation fragment")
	}

	fmt.Println(result)
	return nil
}

func printECSTaskDefinition(cmd *cobra.Command, args []string) error {
	err := checkAPIKeyAndProjectID()
	if err != nil {
		return err
	}

	const isEssential = true
	agentContainer := makeAgentContainerDefinition(
		optionals.None[string](),
		optionals.None[string](),
		optionals.None[string](),
		isEssential,
	)

	// XXX If we instantiate any new fields in the task definition here, we need
	// to remember to update the code in the ecs_console_utils package.
	taskDefinition := types.TaskDefinition{
		ContainerDefinitions: []types.ContainerDefinition{agentContainer},
		Family:               aws.String("postman-insights-agent"),
		NetworkMode:          types.NetworkModeHost,
		RequiresCompatibilities: []types.Compatibility{
			types.CompatibilityEc2,
		},
		Cpu:    aws.String("512"),
		Memory: aws.String("512"),
		RuntimePlatform: &types.RuntimePlatform{
			CpuArchitecture:       types.CPUArchitectureX8664,
			OperatingSystemFamily: types.OSFamilyLinux,
		},
	}

	result, err := ecs_console_utils.TaskDefinitionToJSONForConsole(taskDefinition)
	if err != nil {
		return errors.Wrapf(err, "unable to format task definition as JSON")
	}

	fmt.Println(string(result))
	return nil
}
