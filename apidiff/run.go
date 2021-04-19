package apidiff

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"time"

	"github.com/pkg/errors"

	"github.com/akitasoftware/akita-cli/rest"
	"github.com/akitasoftware/akita-cli/util"
	"github.com/akitasoftware/akita-libs/akid"
	"github.com/akitasoftware/akita-libs/akiuri"
	"github.com/akitasoftware/akita-libs/path_trie"
)

func Run(args Args) error {
	frontClient := rest.NewFrontClient(args.Domain, args.ClientID)

	// Resolve service ID.
	serviceName := args.BaseSpecURI.ServiceName
	if serviceName != args.NewSpecURI.ServiceName {
		return errors.Errorf("only support diffing specs from the same service for now")
	}

	serviceID, err := util.GetServiceIDByName(frontClient, serviceName)
	if err != nil {
		return err
	}
	learnClient := rest.NewLearnClient(args.Domain, args.ClientID, serviceID)

	// Resolve API spec IDs
	baseSpecID, err := resolveAPISpecID(learnClient, args.BaseSpecURI)
	if err != nil {
		return errors.Wrapf(err, "failed to resolve ID for %s", args.BaseSpecURI)
	}
	newSpecID, err := resolveAPISpecID(learnClient, args.NewSpecURI)
	if err != nil {
		return errors.Wrapf(err, "failed to resolve ID for %s", args.NewSpecURI)
	}

	// TODO(kku): make the timeout tunable
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	diff, err := learnClient.GetSpecDiffTrie(ctx, baseSpecID, newSpecID)
	if err != nil {
		return errors.Wrap(err, "failed to get diff")
	}

	if args.Out != "" {
		return writeJSON(args.Out, diff)
	}
	return interactiveDisplay(diff)
}

func resolveAPISpecID(lc rest.LearnClient, uri akiuri.URI) (akid.APISpecID, error) {
	if !uri.ObjectType.IsSpec() || uri.ObjectName == "" {
		return akid.APISpecID{}, errors.Errorf("%s does not refer to an API spec", uri)
	}

	// TODO(kku): make the timeout tunable
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return lc.GetAPISpecIDByName(ctx, uri.ObjectName)
}

func writeJSON(out string, diff *path_trie.PathTrie) error {
	var w io.Writer
	if out == "-" {
		w = os.Stdout
	} else {
		f, err := os.OpenFile(out, os.O_RDWR|os.O_CREATE, 0644)
		if err != nil {
			return errors.Wrapf(err, "failed to open %q", out)
		}
		defer f.Close()
		w = f
	}

	enc := json.NewEncoder(w)
	return enc.Encode(diff)
}
