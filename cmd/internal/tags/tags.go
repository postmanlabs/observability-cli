package tags

import (
	"strings"

	"github.com/pkg/errors"
)

// Returns a map from parsing a list of "key=value" pairs.
func FromPairs(pairs []string) (map[string]string, error) {
	results := make(map[string]string, len(pairs))
	for _, p := range pairs {
		parts := strings.Split(p, "=")
		if len(parts) != 2 {
			return nil, errors.Errorf("%s is not a valid key=value format", p)
		}

		k, v := parts[0], parts[1]
		if _, ok := results[k]; ok {
			return nil, errors.Errorf("tag with key %s specified more than once", k)
		}

		if strings.HasPrefix(strings.ToLower(k), "x-akita") {
			return nil, errors.New(`Tags starting with "x-akita" are reserved for Akita internal use.`)
		}

		results[k] = v
	}
	return results, nil
}
