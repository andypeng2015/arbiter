# Arbiter developer tasks.
.PHONY: proto generate test build

# Regenerate the gRPC API (api/arbiter/v1/*.pb.go) from proto/arbiter/v1/*.proto.
# Requires protoc, protoc-gen-go, and protoc-gen-go-grpc on PATH. The module
# option strips the go_package module prefix so output lands under api/.
proto:
	protoc -I proto \
		--go_out=. --go_opt=module=m31labs.dev/arbiter \
		--go-grpc_out=. --go-grpc_opt=module=m31labs.dev/arbiter \
		proto/arbiter/v1/service.proto \
		proto/arbiter/v1/capability.proto

# Regenerate the embedded parser table (grammar.bin) and grammar.json.
generate:
	go generate ./...

build:
	go build ./...

test:
	go test ./...
