package nginx

import (
	"fmt"
	"net/http"
	"os"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/spf13/cobra"
)

type dumpToConsole struct{}

func (_ dumpToConsole) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	// httputil.DumpRequest(req, true)
	req.Write(os.Stdout)
	fmt.Println("\n-----------------------------------------------")

	rw.WriteHeader(200)
	rw.Header().Set("Content-type", "text/plain")
	rw.Write([]byte("OK"))
}

// Open a web server that dumps the output to the console and returns 200
// for everything. Used for NGINX module development as a sink for its HTTP calls.
// TODO: a way to inject errors
// TODO: start recognizing the actual REST types
func runDevelopmentServer(cmd *cobra.Command, args []string) error {
	printer.Infof("Listening on port %d in development mode...\n", listenPortFlag)

	server := dumpToConsole{}
	listenAddress := fmt.Sprintf(":%d", listenPortFlag)
	return http.ListenAndServe(listenAddress, server)
}
