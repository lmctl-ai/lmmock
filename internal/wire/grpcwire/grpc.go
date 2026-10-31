// Package grpcwire adapts descriptor-selected protobuf messages to lmcode.
// No application method, message type, reply or sequencing rule is compiled in Go.
package grpcwire

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"strings"

	"github.com/lmctl-ai/lmmock/internal/wire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	_ "google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
)

type Options struct {
	DescriptorSet string `json:"descriptor_set"`
	SHA256        string `json:"descriptor_sha256"`
	Encoding      string `json:"descriptor_encoding,omitempty"`
}
type Layer struct {
	types   *dynamicpb.Types
	methods map[string]protoreflect.MethodDescriptor
}

func init() { wire.Register("grpc", wire.Definition{SAM: "GRPCHandler", New: New}) }
func New(base string, raw json.RawMessage) (wire.Layer, error) {
	var options Options
	if err := wire.Decode(raw, &options); err != nil {
		return nil, err
	}
	if options.DescriptorSet == "" {
		return nil, errors.New("gRPC scenario requires descriptor_set")
	}
	expected, err := hex.DecodeString(options.SHA256)
	if err != nil || len(expected) != sha256.Size {
		return nil, errors.New("gRPC scenario requires descriptor_sha256 (64 hex digits)")
	}
	data, err := os.ReadFile(wire.Resolve(base, options.DescriptorSet))
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	if !bytes.Equal(expected, digest[:]) {
		return nil, errors.New("gRPC descriptor SHA256 mismatch")
	}
	var set descriptorpb.FileDescriptorSet
	switch options.Encoding {
	case "", "binary":
		err = proto.Unmarshal(data, &set)
	case "json":
		err = protojson.Unmarshal(data, &set)
	default:
		return nil, errors.New("descriptor_encoding must be binary or json")
	}
	if err != nil {
		return nil, fmt.Errorf("gRPC descriptor decode: %w", err)
	}
	files, err := protodesc.NewFiles(&set)
	if err != nil {
		return nil, fmt.Errorf("gRPC descriptor graph: %w", err)
	}
	layer := &Layer{types: dynamicpb.NewTypes(files), methods: map[string]protoreflect.MethodDescriptor{}}
	files.RangeFiles(func(file protoreflect.FileDescriptor) bool {
		services := file.Services()
		for i := 0; i < services.Len(); i++ {
			service := services.Get(i)
			methods := service.Methods()
			for j := 0; j < methods.Len(); j++ {
				method := methods.Get(j)
				layer.methods["/"+string(service.FullName())+"/"+string(method.Name())] = method
			}
		}
		return true
	})
	if len(layer.methods) == 0 {
		return nil, errors.New("gRPC descriptor set declares no service methods")
	}
	return layer, nil
}
func (*Layer) Scheme() string { return "grpc" }
func (l *Layer) ValidateRoute(key string) error {
	if key == "*" {
		return nil
	}
	if l.methods[key] == nil {
		return fmt.Errorf("gRPC descriptor has no method %q", key)
	}
	return nil
}
func (l *Layer) Serve(ctx context.Context, ln net.Listener, e *wire.Engine) error {
	server := grpc.NewServer(grpc.ForceServerCodec(codec{l.types}), grpc.MaxRecvMsgSize(math.MaxInt), grpc.MaxSendMsgSize(math.MaxInt), grpc.UnknownServiceHandler(func(_ any, stream grpc.ServerStream) error {
		method, ok := grpc.MethodFromServerStream(stream)
		descriptor := l.methods[method]
		if !ok || descriptor == nil || !e.Has(method) {
			return status.Error(codes.Unimplemented, "unbound gRPC method "+method)
		}
		s := &session{stream: stream, method: descriptor, types: l.types}
		if !descriptor.IsStreamingClient() {
			if err := s.prepareUnary(); err != nil {
				return err
			}
		}
		if err := e.Dispatch(method, s); err != nil {
			return protocolError(err)
		}
		return s.complete()
	}))
	defer server.Stop()
	// Go's HTTP/2 server owns the listener and cancellation. This also avoids
	// introducing grpc-go's separate connection-establishment timer as mock policy.
	return wire.ServeHTTP(ctx, ln, server, wire.HTTP2Protocols())
}
func protocolError(err error) error {
	if _, ok := status.FromError(err); ok {
		return err
	}
	return status.Error(codes.Internal, "lmcode handler: "+err.Error())
}

type session struct {
	pending        bool
	buffered       string
	types          *dynamicpb.Types
	stream         grpc.ServerStream
	method         protoreflect.MethodDescriptor
	received, sent int
	eof            bool
}

func (s *session) Receive() (string, string, error) {
	if s.pending {
		s.pending = false
		return "message", s.buffered, nil
	}
	return s.read()
}
func (s *session) prepareUnary() error {
	_, body, err := s.read()
	if errors.Is(err, io.EOF) {
		return status.Error(codes.InvalidArgument, "descriptor requires one request message")
	}
	if err != nil {
		return protocolError(err)
	}
	_, _, err = s.read()
	if !errors.Is(err, io.EOF) {
		if err != nil {
			return protocolError(err)
		}
		return status.Error(codes.InvalidArgument, "descriptor permits one request message")
	}
	s.pending = true
	s.buffered = body
	return nil
}
func (s *session) read() (string, string, error) {
	if s.eof {
		return "", "", io.EOF
	}
	message := dynamicpb.NewMessage(s.method.Input())
	err := s.stream.RecvMsg(message)
	if errors.Is(err, io.EOF) {
		s.eof = true
		return "", "", io.EOF
	}
	if err != nil {
		return "", "", err
	}
	s.received++
	if !s.method.IsStreamingClient() && s.received > 1 {
		return "", "", status.Error(codes.InvalidArgument, "descriptor permits one request message")
	}
	if unknown(message.ProtoReflect(), s.types) {
		return "", "", status.Error(codes.InvalidArgument, "request contains fields absent from the supplied descriptor")
	}
	body, err := (protojson.MarshalOptions{Resolver: s.types, EmitDefaultValues: true}).Marshal(message)
	return "message", string(body), err
}
func (s *session) Send(kind, body string) error {
	if kind != "" {
		return status.Error(codes.Internal, "gRPC sends protobuf JSON messages, not binary WebSocket frames")
	}
	if !s.method.IsStreamingServer() && s.sent != 0 {
		return status.Error(codes.Internal, "descriptor permits one response message")
	}
	message := dynamicpb.NewMessage(s.method.Output())
	if err := (protojson.UnmarshalOptions{Resolver: s.types}).Unmarshal([]byte(body), message); err != nil {
		return status.Error(codes.Internal, "response descriptor mismatch: "+err.Error())
	}
	if err := s.stream.SendMsg(message); err != nil {
		return err
	}
	s.sent++
	return nil
}
func (s *session) Header(name string) string {
	md, _ := metadata.FromIncomingContext(s.stream.Context())
	v := md.Get(name)
	if len(v) == 0 {
		return ""
	}
	return v[0]
}
func (s *session) SetHeader(name, value string) error {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, ":") || strings.HasPrefix(lower, "grpc-") || lower == "content-type" || lower == "te" || lower == "user-agent" {
		return errors.New("metadata key is owned by the gRPC transport")
	}
	return s.stream.SetHeader(metadata.Pairs(name, value))
}
func (s *session) Fail(code int, message string) error {
	if code < 1 || code > 16 {
		return errors.New("gRPC failure code must be 1..16")
	}
	return status.Error(codes.Code(code), message)
}
func (s *session) complete() error {
	// Unary/server-streaming input was validated before dispatch. Streaming
	// handlers decide when to stop receiving; returning ends their response
	// stream without waiting for a peer half-close or inventing a deadline.
	if !s.method.IsStreamingClient() && s.received != 1 {
		return status.Error(codes.InvalidArgument, "descriptor requires one request message")
	}
	if !s.method.IsStreamingServer() && s.sent != 1 {
		return status.Error(codes.Internal, "descriptor requires one response message")
	}
	return nil
}
func unknown(m protoreflect.Message, types *dynamicpb.Types) bool {
	if len(m.GetUnknown()) != 0 {
		return true
	}
	// Any carries a nested wire message inside bytes; validate it before JSON
	// conversion can discard unknown fields from that nested payload.
	if m.Descriptor().FullName() == "google.protobuf.Any" {
		fields := m.Descriptor().Fields()
		urlField, valueField := fields.ByName("type_url"), fields.ByName("value")
		if urlField == nil || valueField == nil || urlField.Kind() != protoreflect.StringKind || valueField.Kind() != protoreflect.BytesKind {
			return true
		}
		url, data := m.Get(urlField).String(), m.Get(valueField).Bytes()
		if url != "" || len(data) != 0 {
			kind, err := types.FindMessageByURL(url)
			if err != nil {
				return true
			}
			child := kind.New().Interface()
			if err = (proto.UnmarshalOptions{Resolver: types}).Unmarshal(data, child); err != nil {
				return true
			}
			if unknown(child.ProtoReflect(), types) {
				return true
			}
		}
	}
	bad := false
	m.Range(func(f protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		if f.IsMap() {
			if f.MapValue().Message() != nil {
				v.Map().Range(func(_ protoreflect.MapKey, v protoreflect.Value) bool { bad = unknown(v.Message(), types); return !bad })
			}
		}
		if f.IsList() && f.Message() != nil {
			for i := 0; i < v.List().Len() && !bad; i++ {
				bad = unknown(v.List().Get(i).Message(), types)
			}
		}
		if !f.IsList() && !f.IsMap() && f.Message() != nil {
			bad = unknown(v.Message(), types)
		}
		return !bad
	})
	return bad
}
