package arbiter_test

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"

	arbiter "m31labs.dev/arbiter"
	"m31labs.dev/arbiter/protoschema"
)

// orderDescriptorSet builds a serialized FileDescriptorSet for
// message acme.Order { string id; double total; }
func orderDescriptorSet(t *testing.T) []byte {
	t.Helper()
	optional := descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum()
	set := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{{
		Name:    proto.String("order.proto"),
		Syntax:  proto.String("proto3"),
		Package: proto.String("acme"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("Order"),
			Field: []*descriptorpb.FieldDescriptorProto{
				{Name: proto.String("id"), Number: proto.Int32(1), Label: optional, Type: descriptorpb.FieldDescriptorProto_TYPE_STRING.Enum()},
				{Name: proto.String("total"), Number: proto.Int32(2), Label: optional, Type: descriptorpb.FieldDescriptorProto_TYPE_DOUBLE.Enum()},
			},
		}},
	}}}
	data, err := proto.Marshal(set)
	if err != nil {
		t.Fatalf("marshal descriptor set: %v", err)
	}
	return data
}

// The headline: a .arb compiles type-checked against a .proto with no input{}
// block, and a typo'd field name is caught at compile time.
func TestProtoBindingEndToEnd(t *testing.T) {
	schema, err := protoschema.FromFileDescriptorSet(orderDescriptorSet(t), "acme.Order")
	if err != nil {
		t.Fatalf("FromFileDescriptorSet: %v", err)
	}

	const valid = `rule BigOrder {
    when { total >= 100 and id != "" }
    then Flag { reason: "large" }
}`
	if _, err := arbiter.Compile([]byte(valid), arbiter.WithInputSchema(schema)); err != nil {
		t.Fatalf("valid .arb against bound .proto schema should compile, got: %v", err)
	}

	const typo = `rule BigOrder {
    when { totl >= 100 }
    then Flag { reason: "large" }
}`
	_, err = arbiter.Compile([]byte(typo), arbiter.WithInputSchema(schema))
	if err == nil {
		t.Fatal("typo'd field 'totl' should be rejected against the bound schema, got nil")
	}
	if !strings.Contains(err.Error(), "totl") {
		t.Fatalf("error should name the offending field 'totl', got: %v", err)
	}
}
