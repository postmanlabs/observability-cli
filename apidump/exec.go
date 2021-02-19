package apidump

import (
	"io"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"syscall"

	"github.com/pkg/errors"
)

func runCommand(username string, c string) error {
	var runUser *user.User
	var err error
	if username == "" {
		runUser, err = user.Current()
	} else {
		runUser, err = user.Lookup(username)
	}
	if err != nil {
		if username == "" {
			return errors.Wrap(err, "failed to lookup current user to execute the subcommand")
		}
		return errors.Wrapf(err, "failed to lookup %q to execute the subcommand", username)
	}

	// Only support POSIX systems for now, which means we can assume uid and gid
	// are integers.
	var uid, gid int
	uid, err = strconv.Atoi(runUser.Uid)
	if err != nil {
		return errors.Wrapf(err, "cannot lookup uid for %q", runUser.Name)
	}
	gid, err = strconv.Atoi(runUser.Gid)
	if err != nil {
		return errors.Wrapf(err, "cannot lookup gid for %q", runUser.Name)
	}

	// On UNIX-like systems, uid 0 is almost always root.
	// https://pubs.opengroup.org/onlinepubs/9699919799/
	if username == "" && uid == 0 {
		return errors.Errorf(`implicitly running as root (uid=0), if you really want to run as root, use -u flag to set the username explicitly`)
	}

	args := strings.Split(c, " ")
	if len(args) == 0 {
		return errors.Errorf("empty command")
	}

	cmd := exec.Command(args[0], args[1:]...)
	cmd.SysProcAttr = &syscall.SysProcAttr{}
	cmd.SysProcAttr.Credential = &syscall.Credential{Uid: uint32(uid), Gid: uint32(gid)}

	{
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return errors.Wrap(err, "failed to open stdout pipe")
		}
		go func() { io.Copy(os.Stdout, stdout) }()
	}

	{
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return errors.Wrap(err, "failed to open stderr pipe")
		}
		go func() { io.Copy(os.Stderr, stderr) }()
	}

	if err := cmd.Start(); err != nil {
		return errors.Wrap(err, "failed to start")
	}

	return cmd.Wait()
}
