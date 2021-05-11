package trace

import (
	pb "github.com/akitasoftware/akita-ir/go/api_spec"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-libs/spec_util"
	. "github.com/akitasoftware/akita-libs/visitors"
	vis "github.com/akitasoftware/akita-libs/visitors/http_rest"
)

func obfuscate(m *pb.Method) {
	var ov obfuscationVisitor
	vis.Apply(&ov, m)
}

type obfuscationVisitor struct {
	vis.DefaultHttpRestSpecVisitor
}

func (ov *obfuscationVisitor) EnterData(
	ctx vis.HttpRestSpecVisitorContext,
	d *pb.Data,
) Cont {
	dp, isPrimitive := d.GetValue().(*pb.Data_Primitive)
	if !isPrimitive {
		return Continue
	}

	pv, err := spec_util.PrimitiveValueFromProto(dp.Primitive)
	if err != nil {
		printer.Warningf("failed to obfuscate raw value, dropping\n")
		d.Value = nil
		return Continue
	}

	dp.Primitive.Value = pv.Obfuscate().ToProto().Value
	return Continue
}
