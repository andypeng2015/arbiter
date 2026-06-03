package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

func writeOrderDescriptorSet(t *testing.T, path string) {
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
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write descriptor set: %v", err)
	}
}

func TestCheckBindsProtoSchema(t *testing.T) {
	dir := t.TempDir()
	setPath := filepath.Join(dir, "order.binpb")
	writeOrderDescriptorSet(t, setPath)
	arbPath := filepath.Join(dir, "rules.arb")

	// A typo'd field name is caught against the bound .proto schema.
	if err := os.WriteFile(arbPath, []byte("rule R { when { totl >= 100 } then Flag {} }"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runCheck([]string{arbPath, "--proto", setPath, "--message", "acme.Order"})
	if err == nil || !strings.Contains(err.Error(), "totl") {
		t.Fatalf("want compile error naming 'totl', got: %v", err)
	}

	// A valid field reference passes.
	if err := os.WriteFile(arbPath, []byte("rule R { when { total >= 100 } then Flag {} }"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCheck([]string{arbPath, "--proto", setPath, "--message", "acme.Order"}); err != nil {
		t.Fatalf("valid field should pass: %v", err)
	}
}

func TestCheckBindsRawProtoFile(t *testing.T) {
	dir := t.TempDir()
	protoPath := filepath.Join(dir, "order.proto")
	if err := os.WriteFile(protoPath, []byte(`syntax = "proto3";
package acme;
message Order { string id = 1; double total = 2; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	arbPath := filepath.Join(dir, "rules.arb")

	if err := os.WriteFile(arbPath, []byte("rule R { when { totl >= 100 } then Flag {} }"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := runCheck([]string{arbPath, "--proto", protoPath, "--message", "acme.Order"})
	if err == nil || !strings.Contains(err.Error(), "totl") {
		t.Fatalf("want compile error naming 'totl' against raw .proto, got: %v", err)
	}

	if err := os.WriteFile(arbPath, []byte("rule R { when { total >= 100 } then Flag {} }"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCheck([]string{arbPath, "--proto", protoPath, "--message", "acme.Order"}); err != nil {
		t.Fatalf("valid field against raw .proto should pass: %v", err)
	}
}

func TestCheckInLanguageInputFromProto(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "order.proto"), []byte(`syntax = "proto3";
package acme;
message Order { string id = 1; double total = 2; }`), 0o644); err != nil {
		t.Fatal(err)
	}
	arbPath := filepath.Join(dir, "rules.arb")

	if err := os.WriteFile(arbPath, []byte(`input from proto "order.proto" message "acme.Order"
rule R { when { totl >= 1 } then F {} }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCheck([]string{arbPath}); err == nil || !strings.Contains(err.Error(), "totl") {
		t.Fatalf("check should resolve in-language `input from proto` and reject 'totl', got: %v", err)
	}

	if err := os.WriteFile(arbPath, []byte(`input from proto "order.proto" message "acme.Order"
rule R { when { total >= 1 } then F {} }`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCheck([]string{arbPath}); err != nil {
		t.Fatalf("valid in-language `input from proto` should pass check: %v", err)
	}
}

func TestCheckProtoFlagsRequireBoth(t *testing.T) {
	dir := t.TempDir()
	arbPath := filepath.Join(dir, "rules.arb")
	if err := os.WriteFile(arbPath, []byte("rule R { when { x >= 1 } then F {} }"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runCheck([]string{arbPath, "--proto", "x.binpb"}); err == nil {
		t.Fatal("want error when --proto given without --message")
	}
}
