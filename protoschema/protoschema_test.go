package protoschema

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"

	"m31labs.dev/arbiter/ir"
)

// fileWith builds a FileDescriptor from a FileDescriptorProto for use in tests.
func fileWith(t *testing.T, fdp *descriptorpb.FileDescriptorProto) protoreflect.FileDescriptor {
	t.Helper()
	fd, err := protodesc.NewFile(fdp, nil)
	if err != nil {
		t.Fatalf("protodesc.NewFile: %v", err)
	}
	return fd
}

func msg(t *testing.T, fd protoreflect.FileDescriptor, name string) protoreflect.MessageDescriptor {
	t.Helper()
	md := fd.Messages().ByName(protoreflect.Name(name))
	if md == nil {
		t.Fatalf("message %q not found in descriptor", name)
	}
	return md
}

func field(name string, num int32, typ descriptorpb.FieldDescriptorProto_Type) *descriptorpb.FieldDescriptorProto {
	return &descriptorpb.FieldDescriptorProto{
		Name:   proto.String(name),
		Number: proto.Int32(num),
		Label:  descriptorpb.FieldDescriptorProto_LABEL_OPTIONAL.Enum(),
		Type:   typ.Enum(),
	}
}

// findField returns the schema field with the given name, or fails.
func findField(t *testing.T, s *ir.InputSchema, name string) ir.SchemaField {
	t.Helper()
	for _, f := range s.Fields {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("field %q not present in synthesized schema (have %v)", name, fieldNames(s.Fields))
	return ir.SchemaField{}
}

func fieldNames(fs []ir.SchemaField) []string {
	names := make([]string, len(fs))
	for i, f := range fs {
		names[i] = f.Name
	}
	return names
}

func msgField(name string, num int32, typeName string) *descriptorpb.FieldDescriptorProto {
	f := field(name, num, descriptorpb.FieldDescriptorProto_TYPE_MESSAGE)
	f.TypeName = proto.String(typeName)
	return f
}

func enumField(name string, num int32, typeName string) *descriptorpb.FieldDescriptorProto {
	f := field(name, num, descriptorpb.FieldDescriptorProto_TYPE_ENUM)
	f.TypeName = proto.String(typeName)
	return f
}

func repeated(f *descriptorpb.FieldDescriptorProto) *descriptorpb.FieldDescriptorProto {
	f.Label = descriptorpb.FieldDescriptorProto_LABEL_REPEATED.Enum()
	return f
}

// richFile builds a file exercising nested messages, repeated fields, enums,
// and a self-referential message (cycle).
func richFile(t *testing.T) protoreflect.FileDescriptor {
	return fileWith(t, &descriptorpb.FileDescriptorProto{
		Name:    proto.String("rich.proto"),
		Syntax:  proto.String("proto3"),
		Package: proto.String("acme"),
		MessageType: []*descriptorpb.DescriptorProto{
			{
				Name: proto.String("User"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("email", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING),
					msgField("address", 2, ".acme.Address"),
					repeated(field("tags", 3, descriptorpb.FieldDescriptorProto_TYPE_STRING)),
					repeated(msgField("contacts", 4, ".acme.Address")),
					enumField("role", 5, ".acme.Role"),
				},
			},
			{
				Name: proto.String("Address"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("city", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING),
					field("zip", 2, descriptorpb.FieldDescriptorProto_TYPE_STRING),
				},
			},
			{
				Name: proto.String("Node"),
				Field: []*descriptorpb.FieldDescriptorProto{
					field("name", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING),
					msgField("parent", 2, ".acme.Node"),
				},
			},
		},
		EnumType: []*descriptorpb.EnumDescriptorProto{{
			Name: proto.String("Role"),
			Value: []*descriptorpb.EnumValueDescriptorProto{
				{Name: proto.String("ROLE_UNKNOWN"), Number: proto.Int32(0)},
				{Name: proto.String("ADMIN"), Number: proto.Int32(1)},
			},
		}},
	})
}

func TestFromMessageNestedMessage(t *testing.T) {
	schema, err := FromMessage(msg(t, richFile(t), "User"))
	if err != nil {
		t.Fatalf("FromMessage: %v", err)
	}
	addr := findField(t, schema, "address")
	if addr.Type.Base != "object" {
		t.Fatalf("address base = %q, want object", addr.Type.Base)
	}
	if len(addr.Children) != 2 {
		t.Fatalf("address children = %d, want 2 (%v)", len(addr.Children), fieldNames(addr.Children))
	}
	city := findFieldIn(t, addr.Children, "city")
	if city.Type.Base != "string" {
		t.Errorf("address.city base = %q, want string", city.Type.Base)
	}
}

func TestFromMessageRepeatedScalar(t *testing.T) {
	schema, err := FromMessage(msg(t, richFile(t), "User"))
	if err != nil {
		t.Fatalf("FromMessage: %v", err)
	}
	tags := findField(t, schema, "tags")
	if tags.Type.Base != "list" {
		t.Fatalf("tags base = %q, want list", tags.Type.Base)
	}
	if tags.Type.Element == nil || tags.Type.Element.Base != "string" {
		t.Fatalf("tags element = %+v, want string", tags.Type.Element)
	}
}

func TestFromMessageRepeatedMessageIsListOfObject(t *testing.T) {
	schema, err := FromMessage(msg(t, richFile(t), "User"))
	if err != nil {
		t.Fatalf("FromMessage: %v", err)
	}
	contacts := findField(t, schema, "contacts")
	if contacts.Type.Base != "list" {
		t.Fatalf("contacts base = %q, want list", contacts.Type.Base)
	}
	if contacts.Type.Element == nil || contacts.Type.Element.Base != "object" {
		t.Fatalf("contacts element = %+v, want object", contacts.Type.Element)
	}
}

func TestFromMessageEnumIsString(t *testing.T) {
	schema, err := FromMessage(msg(t, richFile(t), "User"))
	if err != nil {
		t.Fatalf("FromMessage: %v", err)
	}
	role := findField(t, schema, "role")
	if role.Type.Base != "string" {
		t.Fatalf("role base = %q, want string", role.Type.Base)
	}
}

func TestFromMessageRecursiveMessageTerminates(t *testing.T) {
	// Must not infinite-loop on a self-referential message.
	schema, err := FromMessage(msg(t, richFile(t), "Node"))
	if err != nil {
		t.Fatalf("FromMessage: %v", err)
	}
	parent := findField(t, schema, "parent")
	if parent.Type.Base != "object" {
		t.Fatalf("parent base = %q, want object", parent.Type.Base)
	}
	// The recursive level is emitted as an open object with no further children.
	if len(parent.Children) != 2 {
		t.Fatalf("parent children = %d, want 2 (name, parent)", len(parent.Children))
	}
	inner := findFieldIn(t, parent.Children, "parent")
	if inner.Type.Base != "object" || len(inner.Children) != 0 {
		t.Fatalf("inner parent = base %q children %d, want object with 0 children (cycle break)", inner.Type.Base, len(inner.Children))
	}
}

func findFieldIn(t *testing.T, fs []ir.SchemaField, name string) ir.SchemaField {
	t.Helper()
	for _, f := range fs {
		if f.Name == name {
			return f
		}
	}
	t.Fatalf("field %q not present (have %v)", name, fieldNames(fs))
	return ir.SchemaField{}
}

// scalarUserFDP is a proto3 file with one message of assorted scalar fields.
func scalarUserFDP() *descriptorpb.FileDescriptorProto {
	return &descriptorpb.FileDescriptorProto{
		Name:    proto.String("test.proto"),
		Syntax:  proto.String("proto3"),
		Package: proto.String("acme"),
		MessageType: []*descriptorpb.DescriptorProto{{
			Name: proto.String("User"),
			Field: []*descriptorpb.FieldDescriptorProto{
				field("email", 1, descriptorpb.FieldDescriptorProto_TYPE_STRING),
				field("age", 2, descriptorpb.FieldDescriptorProto_TYPE_INT32),
				field("active", 3, descriptorpb.FieldDescriptorProto_TYPE_BOOL),
				field("score", 4, descriptorpb.FieldDescriptorProto_TYPE_DOUBLE),
				field("balance", 5, descriptorpb.FieldDescriptorProto_TYPE_INT64),
			},
		}},
	}
}

func TestFromFileDescriptorSet(t *testing.T) {
	set := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{scalarUserFDP()}}
	data, err := proto.Marshal(set)
	if err != nil {
		t.Fatalf("marshal descriptor set: %v", err)
	}

	schema, err := FromFileDescriptorSet(data, "acme.User")
	if err != nil {
		t.Fatalf("FromFileDescriptorSet: %v", err)
	}
	if len(schema.Fields) != 5 {
		t.Fatalf("got %d fields %v, want 5", len(schema.Fields), fieldNames(schema.Fields))
	}
	if f := findField(t, schema, "email"); f.Type.Base != "string" {
		t.Errorf("email base = %q, want string", f.Type.Base)
	}
}

func TestFromFileDescriptorSetUnknownMessage(t *testing.T) {
	set := &descriptorpb.FileDescriptorSet{File: []*descriptorpb.FileDescriptorProto{scalarUserFDP()}}
	data, err := proto.Marshal(set)
	if err != nil {
		t.Fatalf("marshal descriptor set: %v", err)
	}
	if _, err := FromFileDescriptorSet(data, "acme.Nope"); err == nil {
		t.Fatal("expected error for message absent from descriptor set, got nil")
	}
}

func TestFromFileDescriptorSetBadData(t *testing.T) {
	if _, err := FromFileDescriptorSet([]byte("not a descriptor set"), "acme.User"); err == nil {
		t.Fatal("expected error for malformed descriptor set bytes, got nil")
	}
}

func TestFromMessageScalars(t *testing.T) {
	fd := fileWith(t, scalarUserFDP())

	schema, err := FromMessage(msg(t, fd, "User"))
	if err != nil {
		t.Fatalf("FromMessage: %v", err)
	}

	want := map[string]string{
		"email":   "string",
		"age":     "number",
		"active":  "boolean",
		"score":   "number",
		"balance": "number",
	}
	if len(schema.Fields) != len(want) {
		t.Fatalf("got %d fields %v, want %d", len(schema.Fields), fieldNames(schema.Fields), len(want))
	}
	for name, base := range want {
		f := findField(t, schema, name)
		if f.Type.Base != base {
			t.Errorf("field %q: got base %q, want %q", name, f.Type.Base, base)
		}
	}
}
