package ec2

import (
	"embed"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"text/template"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/pkg/errors"
)

const (
	envFileName         = "postman-lc-agent"
	envFileTemplateName = "postman-lc-agent.tmpl"
	envFileBasePath     = "/etc/default/"
	envFilePath         = envFileBasePath + envFileName

	serviceFileName     = "postman-lc-agent.service"
	serviceFileBasePath = "/usr/lib/systemd/system/"
	serviceFilePath     = serviceFileBasePath + serviceFileName
)

// Embed files inside the binary. Requires Go >=1.16

//go:embed postman-lc-agent.service
var serviceFile string

// FS is used for easier template parsing

//go:embed postman-lc-agent.tmpl
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

func checkUserPermissions() error {
	// TODO: Make this work without root

	// Exact permissions required are
	// read/write permissions on /etc/default/postman-lc-agent
	// read/write permission on /usr/lib/system/systemd
	// enable, daemon-reload, start, stop permission for systemctl

	printer.Infof("Checking user permissions \n")
	cu, err := user.Current()
	if err != nil {
		return errors.Errorf("could not get current user. OS returned error: %q \n", err)
	}
	if !strings.EqualFold(cu.Name, "root") {
		return errors.Errorf("Please run the command again with user: root. \n")
	}
	return nil
}

func checkSystemdExists() error {
	message := "Checking if systemd exists"
	printer.Infof(message + "\n")
	reportStep(message)

	_, serr := exec.LookPath("systemd")
	if serr != nil {
		return errors.Errorf("Could not find systemd binary in your OS.\n We don't have support for non-systemd OS as of now; For more information please contact observability-support@postman.com.\n")
	}
	return nil
}

func configureSystemdFiles(collectionId string) error {
	message := "Configuring systemd files"
	printer.Infof(message + "\n")
	reportStep(message)

	// Write collectionId and postman-api-key to go template file

	tmpl, err := template.ParseFS(envFileFS, envFileTemplateName)
	if err != nil {
		return errors.Errorf("failed to parse systemd env template file with error :%q\n Please contact observability-support@postman.com  with this log for further assistance.\n", err)
	}

	data := struct {
		PostmanAPIKey string
		CollectionId  string
	}{
		PostmanAPIKey: os.Getenv("POSTMAN_API_KEY"),
		CollectionId:  collectionId,
	}
	envFile, err := os.Create("env-postman-lc-agent")
	if err != nil {
		return errors.Errorf("Failed to write values to env file with error %q", err)
	}

	err = tmpl.Execute(envFile, data)
	if err != nil {
		return errors.Errorf("Failed to write values to env file with error %q", err)
	}

	// Ensure /etc/default exists
	cmd := exec.Command("mkdir", []string{"-p", envFileBasePath}...)
	_, err = cmd.CombinedOutput()
	if err != nil {
		return errors.Errorf("failed to find %s directory with err %q \n", envFileBasePath, err)
	}

	// move the file to /etc/default
	cmd = exec.Command("mv", []string{"env-postman-lc-agent", envFilePath}...)
	_, err = cmd.CombinedOutput()
	if err != nil {
		return errors.Errorf("failed to create postman-lc-agent env file in /etc/default directory with err %q \n", err)
	}

	// Ensure /usr/lib/systemd/system exists
	cmd = exec.Command("mkdir", []string{"-p", serviceFileBasePath}...)
	_, err = cmd.CombinedOutput()
	if err != nil {
		return errors.Errorf("failed to find %s directory with err %q \n", serviceFileBasePath, err)
	}

	err = os.WriteFile(serviceFilePath, []byte(serviceFile), 0600)
	if err != nil {
		return errors.Errorf("failed to create %s file in %s directory with err %q \n", serviceFileName, serviceFilePath, err)
	}

	return nil
}

// Starts the postman LCA agent as a systemd service
func enablePostmanAgent() error {
	reportStep("Enabling postman-lc-agent as a service")

	cmd := exec.Command("systemctl", []string{"daemon-reload"}...)
	_, err := cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "failed to run systemctl daeomon-reload\n")
	}
	// systemctl start postman-lc-service
	cmd = exec.Command("systemctl", []string{"start", serviceFileName}...)
	_, err = cmd.CombinedOutput()
	if err != nil {
		return errors.Wrapf(err, "failed to run systemctl daeomon-reload\n")
	}

	return nil
}

// Run post-checks
func postChecks() error {
	reportStep("EC2:Running post checks")

	// TODO: How to Verify if traffic is being captured ?
	return nil
}
