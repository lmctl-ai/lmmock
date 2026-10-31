package grpcwire

import (
	"fmt"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/dynamicpb"
)

// codec resolves scenario-defined extensions without mutating process-global
// protobuf registries. The wire encoding is the ordinary gRPC proto encoding.
type codec struct{ types *dynamicpb.Types }

func (codec) Name() string { return "proto" }
func (codec) Marshal(value any) ([]byte, error) {
	message, ok := value.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("not a protobuf message: %T", value)
	}
	return proto.Marshal(message)
}
func (c codec) Unmarshal(data []byte, value any) error {
	message, ok := value.(proto.Message)
	if !ok {
		return fmt.Errorf("not a protobuf message: %T", value)
	}
	return (proto.UnmarshalOptions{Resolver: c.types}).Unmarshal(data, message)
}
