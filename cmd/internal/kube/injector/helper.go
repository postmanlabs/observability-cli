package injector

import (
	"bytes"
	v1 "k8s.io/api/core/v1"
	kyaml "sigs.k8s.io/yaml"
)

// Calls the given injector's Inject method and returns the result as a YAML bytes.
func ToRawYAML(injector Injector, sidecar v1.Container) ([]byte, error) {
	injectedObjects, err := injector.Inject(sidecar)
	if err != nil {
		return nil, err
	}

	out := new(bytes.Buffer)
	for _, obj := range injectedObjects {
		raw, err := kyaml.Marshal(obj)
		if err != nil {
			return nil, err
		}

		out.WriteString("---\n")
		out.Write(raw)
	}

	return out.Bytes(), nil
}
