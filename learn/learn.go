package learn

import (
	pb "github.com/akitasoftware/akita-ir/go/api_spec"
	"github.com/akitasoftware/akita-libs/spec_util"
)

func MergeWitness(dst, src *pb.Witness) {
	if dst.Method == nil {
		dst.Method = src.Method
		return
	}

	if dst.Method.Args == nil {
		dst.Method.Args = src.Method.Args
	} else {
		dst.Method.Responses = src.Method.Responses
	}

	if dst.Method.Meta == nil {
		dst.Method.Meta = src.Method.Meta
	}

	// Special HTTP handling - if dst is a witness of the response, populate HTTP
	// method meta from the src (the request witness).
	if httpMeta := spec_util.HTTPMetaFromMethod(dst.Method); httpMeta != nil && httpMeta.Method == "" {
		dst.Method.Meta = src.Method.Meta
	}
}
