package rest

import (
	"runtime"

	"github.com/akitasoftware/akita-cli/env"
	"github.com/akitasoftware/akita-cli/useragent"
	"github.com/akitasoftware/akita-cli/version"
)

func GetUserAgent() string {
	e := useragent.ENV_HOST
	if env.InDocker() {
		e = useragent.ENV_DOCKER
	}

	ua := useragent.UA{
		Version: version.ReleaseVersion(),
		OS:      runtime.GOOS,
		Arch:    runtime.GOARCH,
		EnvType: e,
	}
	return ua.String()
}
