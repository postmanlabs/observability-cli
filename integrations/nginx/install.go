package nginx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/telemetry"
	"github.com/akitasoftware/go-utils/optionals"
	"github.com/pkg/errors"
)

/* Top-level error to report to the user. */
type InstallationError struct {
	Wrapped error
	Remedy  string
}

func (e *InstallationError) Unwrap() error {
	return e.Wrapped
}

func (e *InstallationError) Error() string {
	return fmt.Sprintf("Couldn't automatically install the Akita NGX module: %v", e.Wrapped)
}

var _ error = (*InstallationError)(nil)

var (
	unsupportedError = &InstallationError{
		Wrapped: errors.New("The version of NGINX that is installed does not have a precompiled module."),
		Remedy:  "Please try building the module from source, using the directions at https://github.com/akitasoftware/akita-nginx-module#building-the-module-from-source",
	}

	uninstalledError = &InstallationError{
		Wrapped: errors.New("NGINX is not installed, or 'nginx' is not in the current path."),
		Remedy:  "Please run this command on the machine where NGINX is installed.",
	}

	downloadError = &InstallationError{
		Wrapped: errors.New("The precompiled module could not be downloaded from Akita."),
		Remedy:  "Please download the module from https://github.com/akitasoftware/akita-nginx-module/releases/latest and complete the installation by copying it into the NGINX module directory.",
	}
)

func newCopyError(path string) error {
	return &InstallationError{
		Wrapped: errors.New("The precompiled module could not be installed into the NGINX directory."),
		Remedy:  fmt.Sprintf("Please complete the installation by copying %s into the NGINX module directory.\n", path),
	}
}

func newSymlinkError(moduleDir, downloadFile string) error {
	return &InstallationError{
		Wrapped: errors.New("The precompiled module was installed, but a symbolic link to it could not be created."),
		Remedy: fmt.Sprintf("Please create a symbolic link in the module directory %s from ngx_http_akita_module.so' to %s.",
			moduleDir, downloadFile),
	}
}

type InstallArgs struct {
	DryRun bool

	// Destination directory for the module
	DestDir optionals.Optional[string]
}

// Install the precompiled NGINX module from the latest release on GitHub.
// If we can't find the Nginx version or platform,
// Return an error to show to the user.
func InstallModule(args *InstallArgs) error {
	version, err := FindNginxVersion()
	if err != nil {
		telemetry.Error("NGINX find version", err)
		return err
	}

	arch, platform, err := FindPlatform()
	if err != nil {
		telemetry.Error("NGINX find platform", err)
		return err
	}

	// Report the version being attempted
	telemetry.InstallIntegrationVersion("NGINX", arch, platform, version)

	// The downside of using Github is we don't get any hierarchy, so it
	// all has to be crammed into the name.
	expectedFilename := fmt.Sprintf("ngx_http_akita_module_%s_%s_%s.so",
		arch, platform, version)
	printer.Debugf("Looking for release artifact %q\n", expectedFilename)

	// Connect to Github and grab the latest release.
	assets, err := GetLatestReleaseAssets()
	if err != nil {
		telemetry.Error("NGINX get release", err)
		return downloadError
	}

	// Find the matching prebuilt modules
	var assetOpt optionals.Optional[GithubAsset]
	for _, a := range assets {
		if a.Name == expectedFilename {
			assetOpt = optionals.Some(a)
			break
		}
	}
	asset, present := assetOpt.Get()
	if !present {
		return unsupportedError
	}

	printer.Infof("Selected prebuilt module %s\n", asset.Name)

	// Find the Nginx directory
	destDir := args.DestDir
	if destDir.IsNone() {
		destDir = FindNginxModuleDir()
	}
	if dest, exists := destDir.Get(); exists {
		printer.Infof("Module will be installed in %v\n", dest)
	} else {
		printer.Warningf("Can't identify directory for module to be installed.\n")
	}

	// Bail out if the user didn't actually ask for the download
	if args.DryRun {
		printer.Infof("Ready to download; skipping due to --dry-run flag.\n")
		return nil
	}

	// Create a temporary directory for download and a shorter filename inside it.
	tmpDir, err := os.MkdirTemp("", "akita-nginx-download")
	if err != nil {
		printer.Errorf("Can't create temporary directory for download: %v\n", err)
		return downloadError
	}
	shortName := fmt.Sprintf("ngx_http_akita_module_%s.so", version)
	downloadFile := filepath.Join(tmpDir, shortName)

	// Delete the temporary directory on function exit, unless we explicitly decide to save it.
	saveDir := false
	defer func() {
		if !saveDir {
			os.RemoveAll(tmpDir)
		}
	}()

	err = DownloadReleaseAsset(asset.ID, downloadFile)
	if err != nil {
		telemetry.Error("NGINX download asset", err)
		return downloadError
	}

	// Move the file to its final location, if any.
	if dir, ok := destDir.Get(); ok {
		destPath := filepath.Join(dir, shortName)
		err = os.Rename(downloadFile, destPath)
		if err != nil {
			printer.Errorf("Error moving module to the NGINX module directory: %v\n", err)
			telemetry.Error("NGINX install module", err)
			saveDir = true
			return newCopyError(downloadFile)
		}

		// Delete any existing symlink; expect failure if it doesn't exist.
		err = os.Remove(filepath.Join(dir, "ngx_http_akita_module.so"))
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			// Probably a permissions problem? Log but keep going.
			printer.Infof("Can't remove old symbolic link: %v\n", err)
			telemetry.Error("NGINX install module", err)
		}

		// Create a symlink with no version number, as recommended
		err = os.Symlink(shortName,
			filepath.Join(dir, "ngx_http_akita_module.so"))
		if err != nil {
			printer.Debugf("Error creating symlink: %v\n", err)
			telemetry.Error("NGINX install module", err)
			return newSymlinkError(dir, shortName)
		}
	} else {
		saveDir = true
		return newCopyError(downloadFile)
	}

	printer.Infof("Module ngx_http_akita_module.so successfully installed!\n")
	printer.Infof("To start using the NGINX module,\n" +
		" 1. Add 'load_module ngx_http_akita_module.so' to the top of your NGINX configuration file.\n" +
		" 2. Add 'akita_enable on;' to the NGINX locations that handle the HTTP traffic you want to monitor.\n" +
		" 3. Run 'akita nginx capture --project <project name>' with the project name you have created in the Akita App.\n" +
		" 4. Start NGINX, or reload the configuration file if it's already running.\n" +
		"See https://docs.akita.software/docs/nginx for a step-by-step guide and an example configuration file.\n")
	return nil
}

// Nginx version output might look like:
// nginx version: nginx/1.23.2
// nginx version: nginx/1.18.0 (Ubuntu)
var versionRe = regexp.MustCompile(`nginx/(\d+\.\d+\.\d+)\s`)

// Determine which version of NGINX is installed; for now we'll just
// use the version command.
func FindNginxVersion() (string, error) {
	_, err := exec.LookPath("nginx")
	if err != nil {
		return "", uninstalledError
	}

	cmd := exec.Command("nginx", "-v")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}

	matches := versionRe.FindSubmatch(output)
	if matches == nil {
		return "", fmt.Errorf("Couldn't parse version in %q", string(output))
	}

	return string(matches[1]), nil
}

// Nginx module location is just guesswork, unfortunately.
var nginxModuleLocations = []string{
	"/usr/local/nginx/modules", // preferred location
	"/usr/lib/nginx/modules",   // an Ubuntu-ism
	"/usr/nginx/modules",       //
	"/etc/nginx/modules",       // an Amazon Linux-ism?
}

func FindNginxModuleDir() optionals.Optional[string] {
	for _, d := range nginxModuleLocations {
		if _, err := os.ReadDir(d); err == nil {
			return optionals.Some(d)
		}
	}
	return optionals.None[string]()
}

// For example:
// Distributor ID:	Ubuntu
// Release:	20.04
var lsbReleaseRe = regexp.MustCompile(`Distributor ID:\s+(.*)\s+Release:\s+([^\n]*)`)

// Execute the lsb_release tool and return an OS string corresponding to the output.
// This is the lowercased version of the distributor ID and release, but with
// periods removed.  (This is an ugly workaround for a CircleCI limitation.)
// for example "ubuntu_2004"
func osIDFromLsbRelease() (string, error) {
	cmd := exec.Command("lsb_release", "-ir")
	output, err := cmd.CombinedOutput()
	if err != nil {
		printer.Debugf("Error running lsb_release: %v\n", err)
		return "", errors.Wrap(err, "Can't execute lsb_release")
	}

	printer.Debugf("lsb_release output:\n%v\n", string(output))

	if matches := lsbReleaseRe.FindSubmatch(output); matches != nil {
		return fmt.Sprintf("%s_%s",
			lcAndStripPeriods(matches[1]),
			lcAndStripPeriods(matches[2])), nil
	}

	return "", errors.Wrap(err, "Can't parse lsb_release output")
}

// Convert the bytes to a lower case string, and remove any periods.
func lcAndStripPeriods(b []byte) string {
	return strings.ReplaceAll(strings.ToLower(string(b)), ".", "")
}

// For example:
// NAME="Amazon Linux"
// VERSION="2"
// ID="amzn"
// ID_LIKE="centos rhel fedora"
var osReleaseIDRe = regexp.MustCompile(`ID="([^\n]*)"`)
var osReleaseVersionRe = regexp.MustCompile(`VERSION="([^\n]*)"`)

// Get the os release ID from an /etc/os-release-format file.
// This is the lowercased version of the ID and VERSION_ID values, but with
// periods removed.
// for example "amzn_2"
func osIDFromReleaseFile(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		printer.Debugf("Error opening %q: %v\n", filename, err)
		return "", err
	}

	buf := make([]byte, 2048)
	n, err := file.Read(buf)
	if err != nil {
		printer.Debugf("Error reading from %q: %v\n", filename, err)
		return "", err
	}

	buf = buf[:n]
	idMatch := osReleaseIDRe.FindSubmatch(buf)
	versionMatch := osReleaseVersionRe.FindSubmatch(buf)
	if idMatch != nil && versionMatch != nil {
		return fmt.Sprintf("%s_%s",
			lcAndStripPeriods(idMatch[1]),
			lcAndStripPeriods(versionMatch[1])), nil
	}
	return "", errors.Wrapf(err, "Can't parse %q", filename)
}

// Order in which to try finding a release identification files.
// /etc/os-release is necessary for Amazon Linux.

var osReleaseFiles = []string{
	"/etc/os-release",
	"/etc/Eos-release",    // TODO: who needs this?
	"/usr/lib/os-release", // Documented fallback
}

// Determine the current platform: architecture, OS, release
// The operating system should be given in a form that matches the names
// we use for official releases.
func FindPlatform() (arch string, os string, err error) {
	switch runtime.GOOS {
	case "linux":
		// Try lsb_release tool first
		if id, err := osIDFromLsbRelease(); err == nil {
			return runtime.GOARCH, id, nil
		}

		// Then reading release identification files
		for _, f := range osReleaseFiles {
			if id, err := osIDFromReleaseFile(f); err == nil {
				return runtime.GOARCH, id, nil
			}
		}

		// TODO: check /etc/issue? Seems unlikely to work.

		return runtime.GOARCH, runtime.GOOS, errors.New("Unrecognized Linux distribution")
	case "darwin":
		// TODO: is there any finer-grain information we need?
		return runtime.GOARCH, runtime.GOOS, nil
	default:
		// We don't even compile the CLI for anything else, but return
		// it so we can log the attempt to telemetry?
		return runtime.GOARCH, runtime.GOOS, nil
	}
}
