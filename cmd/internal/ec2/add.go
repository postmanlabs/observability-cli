package ec2

import (
	"embed"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"text/template"

	"github.com/AlecAivazis/survey/v2"
	"github.com/akitasoftware/akita-cli/consts"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/pkg/errors"
)

const (
	envFileName         = "postman-insights-agent"
	envFileTemplateName = "postman-insights-agent.tmpl"
	envFileBasePath     = "/etc/default/"
	envFilePath         = envFileBasePath + envFileName

	serviceFileName     = "postman-insights-agent.service"
	serviceFileBasePath = "/usr/lib/systemd/system/"
	serviceFilePath     = serviceFileBasePath + serviceFileName

	// Output of command: systemctl is-enabled postman-insights-agent
	// Refer: https://www.freedesktop.org/software/systemd/man/latest/systemctl.html#Exit%20status
	enabled     = "enabled"                                                                                     // exit code: 0
	disabled    = "disabled"                                                                                    // exit code: 1
	nonExisting = "Failed to get unit file state for postman-insights-agent.service: No such file or directory" // exit code: 1
)

// Embed files inside the binary. Requires Go >=1.16

//go:embed postman-insights-agent.service
var serviceFile string

// FS is used for easier template parsing

//go:embed postman-insights-agent.tmpl
var envFileFS embed.FS

// Helper function for reporting telemetry
func reportStep(stepName string) {
	telemetry.WorkflowStep("Starting systemd conguration", stepName)
}

func setupAgentForServer(collectionId string) error {

	err := checkUserPermissions()
	if err != nil {
		return err
	}
	err = checkSystemdExists()
	if err != nil {
		return err
	}

	err = configureSystemdFiles(collectionId)
	if err != nil {
		return err
	}

	err = enablePostmanAgent()
	if err != nil {
		return err
	}

	return nil
}

func askToReconfigure() error {
	var isReconfigure bool

	printer.Infof("postman-insights-agent is already present as a systemd service\n")
	printer.Infof("Helpful commands \n Check status: systemctl status postman-insights-agent \n Disable agent: systemctl disable --now postman-insights-agent \n Check Logs: journalctl -fu postman-insights-agent\n Check env file: cat %s \n Check systemd service file: cat %s \n", envFilePath, serviceFilePath)

	err := survey.AskOne(
		&survey.Confirm{
			Message: "Overwrite old API key and Collection ID values in systemd configuration file with current values?",
			Default: true,
			Help:    "Any edits made to systemd configuration files will be over-written.",
		},
		&isReconfigure,
	)
	if !isReconfigure {
		printer.Infof("Exiting setup \n")
		os.Exit(0)
		return nil
	}
	if err != nil {
		return errors.Wrap(err, "failed to run reconfiguration prompt")
	}
	return nil
}

// Check is systemd service already exists
func checkReconfiguration() error {

	cmd := exec.Command("systemctl", []string{"is-enabled", "postman-insights-agent"}...)
	out, err := cmd.CombinedOutput()

	if err != nil {
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode := exitError.ExitCode()
			if exitCode != 1 {
				return errors.Wrapf(err, "Received non 1 exitcode for systemctl is-enabled. \n Command output:%s \n Please send this log message to %s for assistance\n", out, consts.SupportEmail)
			}
			if strings.Contains(string(out), disabled) {
				return askToReconfigure()
			} else if strings.Contains(string(out), nonExisting) {
				return nil
			}
		}
		return errors.Wrapf(err, "failed to run systemctl is-enabled postman-insights-agent")
	}
	if strings.Contains(string(out), enabled) {
		return askToReconfigure()
	}
	return errors.Errorf("The systemctl is-enabled command produced output this tool doesn't recognize: %q.\nPlease send this log message to %s for assistance\n", string(out), consts.SupportEmail)

}

func checkUserPermissions() error {

	// Exact permissions required are
	// read/write permissions on /etc/default/postman-insights-agent
	// read/write permission on /usr/lib/system/systemd
	// enable, daemon-reload, start, stop permission for systemctl

	printer.Infof("Checking user permissions \n")
	cu, err := user.Current()
	if err != nil {
		return errors.Wrapf(err, "could not get current user")
	}
	if !strings.EqualFold(cu.Name, "root") {
		printer.Errorf("root user is required to setup systemd service and edit related files.\n")
		return errors.Errorf("Please run the command again with root user")
	}
	return nil
}

func checkSystemdExists() error {
	message := "Checking if systemd exists"
	printer.Infof(message + "\n")
	reportStep(message)

	_, serr := exec.LookPath("systemctl")
	if serr != nil {
		printer.Errorf("We don't have support for non-systemd OS as of now.\n For more information please contact %s.\n", consts.SupportEmail)
		return errors.Errorf("Could not find systemd binary in your OS.")
	}
	return nil
}

func configureSystemdFiles(collectionId string) error {
	message := "Configuring systemd files"
	printer.Infof(message + "\n")
	reportStep(message)

	err := checkReconfiguration()
	if err != nil {
		return err
	}

	// Write collectionId and postman-api-key to go template file

	tmpl, err := template.ParseFS(envFileFS, envFileTemplateName)
	if err != nil {
		return errors.Wrapf(err, "systemd env file parsing failed")
	}

	data := struct {
		PostmanAPIKey string
		CollectionId  string
	}{
		PostmanAPIKey: os.Getenv("POSTMAN_API_KEY"),
		CollectionId:  collectionId,
	}

	// Ensure /etc/default exists
	cmd := exec.Command("mkdir", []string{"-p", envFileBasePath}...)
	_, err = cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "failed to create %s directory\n", envFileBasePath)
	}

	envFile, err := os.Create(envFilePath)
	if err != nil {
		printer.Errorf("Failed to create systemd env file")
		return err
	}

	err = tmpl.Execute(envFile, data)
	if err != nil {
		printer.Errorf("Failed to write values to systemd env file")
		return err
	}

	// Ensure /usr/lib/systemd/system exists
	cmd = exec.Command("mkdir", []string{"-p", serviceFileBasePath}...)
	_, err = cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "failed to create %s directory", serviceFileBasePath)
	}

	err = os.WriteFile(serviceFilePath, []byte(serviceFile), 0600)
	if err != nil {
		printer.Errorf("failed to create %s file in %s directory with err %q \n", serviceFileName, serviceFilePath, err)
		return err
	}

	return nil
}

// Starts the Postman Insights Agent as a systemd service
func enablePostmanAgent() error {
	message := "Enabling postman-insights-agent as a service"
	reportStep(message)
	printer.Infof(message + "\n")

	cmd := exec.Command("systemctl", []string{"daemon-reload"}...)
	_, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "failed to run systemctl daemon-reload")
	}
	// systemctl start postman-insights-agent.service
	cmd = exec.Command("systemctl", []string{"enable", "--now", serviceFileName}...)
	_, err = cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "faild to run systemctl enable --now")
	}
	printer.Infof("Postman Insights Agent enabled as a systemd service. Please check logs using the below command \n")
	printer.Infof("journalctl -fu postman-insights-agent \n")

	return nil
}
