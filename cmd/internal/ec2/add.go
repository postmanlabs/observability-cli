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
		return errors.Wrapf(err, "could not get current user\n")
	}
	if !strings.EqualFold(cu.Name, "root") {
		printer.Errorf("root user is required to setup systemd service and edit related files")
		return errors.Errorf("Please run the command again with root user")
	}
	return nil
}

func checkSystemdExists() error {
	message := "Checking if systemd exists"
	printer.Infof(message + "\n")
	reportStep(message)

	_, serr := exec.LookPath("systemd")
	if serr != nil {
		printer.Errorf("We don't have support for non-systemd OS as of now.\n For more information please contact observability-support@postman.com.\n")
		return errors.Errorf("Could not find systemd binary in your OS.")
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
		return errors.Wrapf(err, "failed to create %s directory\n", serviceFileBasePath)
	}

	err = os.WriteFile(serviceFilePath, []byte(serviceFile), 0600)
	if err != nil {
		printer.Errorf("failed to create %s file in %s directory with err %q \n", serviceFileName, serviceFilePath, err)
		return err
	}

	return nil
}

// Starts the postman LCA agent as a systemd service
func enablePostmanAgent() error {
	message := "Enabling postman-lc-agent as a service"
	reportStep(message)
	printer.Infof(message + "\n")

	cmd := exec.Command("systemctl", []string{"daemon-reload"}...)
	_, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	// systemctl start postman-lc-service
	cmd = exec.Command("systemctl", []string{"enable", "--now", serviceFileName}...)
	_, err = cmd.CombinedOutput()
	if err != nil {
		return err
	}
	printer.Infof("Postman LC Agent enabled as a systemd service. Please check logs using \n")
	printer.Infof("journalctl -fu postman-lc-agent")

	return nil
}

// Run post-checks
func postChecks() error {
	reportStep("EC2:Running post checks")

	// TODO: How to Verify if traffic is being captured ?
	return nil
}
