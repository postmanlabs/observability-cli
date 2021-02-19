package trace

import (
	pb "github.com/akitasoftware/akita-ir/go/api_spec"

	"github.com/akitasoftware/akita-cli/printer"
	"github.com/akitasoftware/akita-libs/spec_util"
	"github.com/akitasoftware/akita-libs/visitors/go_ast"
	vis "github.com/akitasoftware/akita-libs/visitors/http_rest"
)

func obfuscate(m *pb.Method) {
	var ov obfuscationVisitor
	vis.Apply(go_ast.PREORDER, &ov, m)
}

type obfuscationVisitor struct {
	vis.DefaultHttpRestSpecVisitor
}

func (ov *obfuscationVisitor) VisitData(
	ctx vis.HttpRestSpecVisitorContext,
	d *pb.Data,
) bool {
	dp, isPrimitive := d.GetValue().(*pb.Data_Primitive)
	if !isPrimitive {
		return true
	}

	pv, err := spec_util.PrimitiveValueFromProto(dp.Primitive)
	if err != nil {
		printer.Warningf("failed to obfuscate raw value, dropping\n")
		d.Value = nil
		return true
	}

	dp.Primitive.Value = pv.Obfuscate().ToProto().Value
	return true
}
