package rest

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-cli/cfg"
	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-cli/version"
	"github.com/akitasoftware/akita-libs/spec_util"
)

const (
	// TODO: Make this tunable.
	defaultClientTimeout = 5 * time.Second
)

var (
	// Shared client to maximize connection re-use.
	// TODO: make this private to the package once kgx package is removed.
	HTTPClient *retryablehttp.Client
)

// Error type for non-2xx HTTP errors.
type HTTPError struct {
	StatusCode int
	Body       []byte
}

func (he HTTPError) Error() string {
	if he.StatusCode == 401 {
		return `Invalid credentials, run "login" or use AKITA_API_KEY_SECRET environment variable. ` +
			`If using with Postman, use POSTMAN_API_KEY environment variable`
	}
	return fmt.Sprintf("received status code %d, body: %s", he.StatusCode, string(he.Body))
}

// Implements retryablehttp LeveledLogger interface using printer.
type printerLogger struct{}

func (printerLogger) Error(f string, args ...interface{}) {
	printer.Errorln(f, args)
}

func (printerLogger) Info(f string, args ...interface{}) {
	printer.Infoln(f, args)
}

func (printerLogger) Debug(f string, args ...interface{}) {
	// Use verbose logging so users don't see every interaction with Akita API by
	// default they enable --debug.
	printer.V(4).Debugln(f, args)
}

func (printerLogger) Warn(f string, args ...interface{}) {
	printer.Warningln(f, args)
}

var initHTTPClientOnce sync.Once

func initHTTPClient() {
	HTTPClient = retryablehttp.NewClient()

	transport := &http.Transport{
		MaxIdleConns:    3,
		IdleConnTimeout: 60 * time.Second,
	}
	if ProxyAddress != "" {
		proxyURL, err := url.Parse(ProxyAddress)
		if err != nil {
			proxyURL = &url.URL{
				Host: ProxyAddress,
			}
		}
		if proxyURL.Scheme == "" {
			proxyURL.Scheme = "http"
		}
		printer.Debugf("Using proxy %v\n", proxyURL)
		transport.Proxy = func(*http.Request) (*url.URL, error) {
			return proxyURL, nil
		}
	}
	transport.TLSClientConfig = &tls.Config{}
	if PermitInvalidCertificate {
		printer.Warningf("Disabling TLS checking; sending traffic without verifying identity of Akita servers.\n")
		transport.TLSClientConfig.InsecureSkipVerify = true
	}
	if ExpectedServerName != "" {
		transport.TLSClientConfig.ServerName = ExpectedServerName
	}

	HTTPClient.HTTPClient = &http.Client{
		Transport: transport,
	}

	HTTPClient.RetryWaitMin = 100 * time.Millisecond
	HTTPClient.RetryWaitMax = 1 * time.Second
	HTTPClient.RetryMax = 3
	HTTPClient.Logger = printerLogger{}
	HTTPClient.ErrorHandler = retryablehttp.PassthroughErrorHandler
}

func sendRequest(ctx context.Context, req *http.Request) ([]byte, error) {
	initHTTPClientOnce.Do(initHTTPClient)

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		c, cancel := context.WithTimeout(ctx, defaultClientTimeout)
		defer cancel()
		ctx = c
	}

	postmanAPIKey, postmanEnv := cfg.GetPostmanAPIKeyAndEnvironment()

	if postmanAPIKey == "" {
		apiKeyID, apiKeySecret := cfg.GetAPIKeyAndSecret()

		if apiKeyID == "" {
			return nil, errors.New(`API key ID not found, run "login" or use AKITA_API_KEY_ID environment variable. ` +
				`If using with Postman, use POSTMAN_API_KEY environment variable`)
		}

		if apiKeySecret == "" {
			return nil, errors.New(`API key secret not found, run "login" or use AKITA_API_KEY_SECRET environment variable. ` +
				`If using with Postman, use POSTMAN_API_KEY environment variable`)
		}

		req.SetBasicAuth(apiKeyID, apiKeySecret)
	} else {
		// Set postman API key as header
		req.Header.Set("x-api-key", postmanAPIKey)

		// Set postman env header if it exists
		if postmanEnv != "" {
			req.Header.Set("x-postman-env", postmanEnv)
		}

	}

	req.Header.Set("user-agent", GetUserAgent())

	// Include the git SHA that this copy of the CLI was built from. Its purpose
	// is two-fold:
	// 1. The presence of this header is used as a heuristic to identify witnesses
	// 		that contain akita's API traffic rather than actual user traffic.
	// 2. As extra debug info, since the CLI semantic version is only incremented
	// 		on release, so there could be many experimental builds from different
	//		git commits with the same semantic version.
	req.Header.Set(spec_util.XAkitaCLIGitVersion, version.GitVersion())

	retryableReq, err := retryablehttp.FromRequest(req)
	if err != nil {
		return nil, errors.Wrap(err, "failed to convert HTTP request into retryable request")
	}
	resp, err := HTTPClient.Do(retryableReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if respBody, err := ioutil.ReadAll(resp.Body); err != nil {
		return nil, errors.Wrap(err, "failed to read response body")
	} else if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, HTTPError{StatusCode: resp.StatusCode, Body: respBody}
	} else {
		return respBody, nil
	}
}
