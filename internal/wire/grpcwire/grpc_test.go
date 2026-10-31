package grpcwire

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/lmctl-ai/lmmock/internal/wire"
	"github.com/lmctl-ai/lmmock/internal/wire/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/encoding/gzip"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
	"google.golang.org/protobuf/types/dynamicpb"
	"google.golang.org/protobuf/types/known/anypb"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func descriptor(t *testing.T) (string, []byte, protoreflect.MessageDescriptor) {
	t.Helper()
	raw, err := os.ReadFile("../../../examples/wire/echo-descriptor.json")
	if err != nil {
		t.Fatal(err)
	}
	var set descriptorpb.FileDescriptorSet
	if err = protojson.Unmarshal(raw, &set); err != nil {
		t.Fatal(err)
	}
	data, err := proto.Marshal(&set)
	if err != nil {
		t.Fatal(err)
	}
	files, err := protodesc.NewFiles(&set)
	if err != nil {
		t.Fatal(err)
	}
	d, err := files.FindDescriptorByName("wiretest.Message")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err = os.WriteFile(filepath.Join(dir, "echo.pb"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return dir, data, d.(protoreflect.MessageDescriptor)
}
func configuration(data []byte) []byte {
	hash := sha256.Sum256(data)
	b, _ := json.Marshal(Options{DescriptorSet: "echo.pb", SHA256: hex.EncodeToString(hash[:])})
	return b
}
func server(t *testing.T, change func(string) string) (*grpc.ClientConn, protoreflect.MessageDescriptor) {
	t.Helper()
	dir, data, message := descriptor(t)
	layer, err := New(dir, configuration(data))
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile("../../../examples/wire/grpc.lm")
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	if change != nil {
		text = change(text)
	}
	e, err := wire.New("grpc", layer, testutil.Compile(t, "grpc", text), nil)
	if err != nil {
		t.Fatal(err)
	}
	address := testutil.Start(t, e)
	c, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithNoProxy())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c, message
}
func message(d protoreflect.MessageDescriptor, text string) *dynamicpb.Message {
	m := dynamicpb.NewMessage(d)
	m.Set(d.Fields().ByName("text"), protoreflect.ValueOfString(text))
	return m
}
func value(m *dynamicpb.Message) string {
	return m.Get(m.Descriptor().Fields().ByName("text")).String()
}
func stream(t *testing.T, c *grpc.ClientConn, method string, client, server bool) grpc.ClientStream {
	t.Helper()
	s, err := c.NewStream(context.Background(), &grpc.StreamDesc{ClientStreams: client, ServerStreams: server}, "/wiretest.Echo/"+method)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func send(t *testing.T, s grpc.ClientStream, m *dynamicpb.Message) {
	t.Helper()
	if err := s.SendMsg(m); err != nil {
		t.Fatal(err)
	}
}
func recv(t *testing.T, s grpc.ClientStream, d protoreflect.MessageDescriptor, want string) {
	t.Helper()
	m := dynamicpb.NewMessage(d)
	if err := s.RecvMsg(m); err != nil || value(m) != want {
		t.Fatalf("response order: got %q, want %q: %v", value(m), want, err)
	}
}
func end(t *testing.T, s grpc.ClientStream, d protoreflect.MessageDescriptor) {
	t.Helper()
	if err := s.RecvMsg(dynamicpb.NewMessage(d)); err != io.EOF {
		t.Fatal("expected stream completion", err)
	}
}
func TestDescriptorDrivenClients(t *testing.T) {
	c, d := server(t, nil)
	ctx := context.Background()
	out := dynamicpb.NewMessage(d)
	if err := c.Invoke(ctx, "/wiretest.Echo/Unary", message(d, "hello λ"), out, grpc.UseCompressor(gzip.Name)); err != nil || value(out) != "hello λ" {
		t.Fatal("unary/compression", value(out), err)
	}
	s := stream(t, c, "ServerStream", false, true)
	send(t, s, message(d, "input"))
	s.CloseSend()
	recv(t, s, d, "first")
	recv(t, s, d, "last")
	end(t, s, d)
	s = stream(t, c, "ClientStream", true, false)
	send(t, s, message(d, "A"))
	send(t, s, message(d, "B"))
	s.CloseSend()
	recv(t, s, d, "AB")
	end(t, s, d)
	s = stream(t, c, "Bidi", true, true)
	send(t, s, message(d, "hello"))
	recv(t, s, d, "ready")
	send(t, s, message(d, "go"))
	recv(t, s, d, "done")
	end(t, s, d)
	s.CloseSend()
}
func TestWrongOrderAndDescriptorMismatchFail(t *testing.T) {
	c, d := server(t, nil)
	s := stream(t, c, "Bidi", true, true)
	send(t, s, message(d, "go"))
	s.CloseSend()
	err := s.RecvMsg(dynamicpb.NewMessage(d))
	if status.Code(err) != codes.FailedPrecondition || !strings.Contains(err.Error(), "expected hello") {
		t.Fatal("accepted wrong request order", err)
	}
	err = c.Invoke(context.Background(), "/wiretest.Echo/BadReply", message(d, "x"), dynamicpb.NewMessage(d))
	if status.Code(err) != codes.Internal || !strings.Contains(err.Error(), "descriptor mismatch") {
		t.Fatal("accepted wrong response descriptor", err)
	}
	bad := message(d, "x")
	bad.SetUnknown(protowire.AppendString(protowire.AppendTag(nil, 99, protowire.BytesType), "unknown"))
	err = c.Invoke(context.Background(), "/wiretest.Echo/Unary", bad, dynamicpb.NewMessage(d))
	if status.Code(err) != codes.InvalidArgument {
		t.Fatal("unknown request field accepted", err)
	}
	s = stream(t, c, "Unary", true, true)
	send(t, s, message(d, "one"))
	send(t, s, message(d, "two"))
	s.CloseSend()
	err = s.RecvMsg(dynamicpb.NewMessage(d))
	if status.Code(err) != codes.InvalidArgument {
		t.Fatal("unary cardinality not enforced", err)
	}
}
func TestDescriptorRefusalsBeforeListen(t *testing.T) {
	dir, data, _ := descriptor(t)
	good := configuration(data)
	var options Options
	json.Unmarshal(good, &options)
	options.SHA256 = strings.Repeat("0", 64)
	bad, _ := json.Marshal(options)
	if _, err := New(dir, bad); err == nil || !strings.Contains(err.Error(), "SHA256 mismatch") {
		t.Fatal("descriptor pin ignored", err)
	}
	layer, err := New(dir, good)
	if err != nil {
		t.Fatal(err)
	}
	body := `@wrong fn wrong(s: Session) -> void implements GRPCHandler {} bind("/wiretest.Echo/Unknown","wrong");`
	if _, err = wire.New("grpc", layer, testutil.Compile(t, "grpc", body), nil); err == nil || !strings.Contains(err.Error(), "descriptor has no method") {
		t.Fatal("unknown method accepted", err)
	}
	invalid := []byte("invalid descriptors")
	os.WriteFile(filepath.Join(dir, "echo.pb"), invalid, 0600)
	if _, err = New(dir, configuration(invalid)); err == nil {
		t.Fatal("accepted malformed descriptors")
	}
}

// Same adapter + different descriptor/handler: no production Go method table changes.
func TestNewMethodNeedsOnlyDescriptorAndHandler(t *testing.T) {
	dir, data, d := descriptor(t)
	var set descriptorpb.FileDescriptorSet
	proto.Unmarshal(data, &set)
	set.File[0].Service[0].Method = append(set.File[0].Service[0].Method, &descriptorpb.MethodDescriptorProto{Name: proto.String("Added"), InputType: proto.String(".wiretest.Message"), OutputType: proto.String(".wiretest.Message")})
	data, _ = proto.Marshal(&set)
	os.WriteFile(filepath.Join(dir, "echo.pb"), data, 0600)
	layer, err := New(dir, configuration(data))
	if err != nil {
		t.Fatal(err)
	}
	body := `@added fn added(s: Session) -> void implements GRPCHandler { s.send("{\"text\":\"new application\"}"); } bind("/wiretest.Echo/Added","added");`
	e, err := wire.New("grpc", layer, testutil.Compile(t, "grpc", body), nil)
	if err != nil {
		t.Fatal(err)
	}
	c, err := grpc.NewClient(testutil.Start(t, e), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithNoProxy())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	out := dynamicpb.NewMessage(d)
	if err = c.Invoke(context.Background(), "/wiretest.Echo/Added", message(d, "ignored"), out); err != nil || value(out) != "new application" {
		t.Fatal(value(out), err)
	}
}

func TestProtoDefaultsAndDynamicAny(t *testing.T) {
	c, d := server(t, nil)
	s := stream(t, c, "ClientStream", true, false)
	send(t, s, message(d, ""))
	send(t, s, message(d, "λ\n\""))
	s.CloseSend()
	recv(t, s, d, "λ\n\"")
	end(t, s, d)
}

func TestDynamicAnyUsesScenarioTypes(t *testing.T) {
	dir, data, d := descriptor(t)
	var set descriptorpb.FileDescriptorSet
	proto.Unmarshal(data, &set)
	set.File[0].Dependency = append(set.File[0].Dependency, "google/protobuf/any.proto")
	set.File = append(set.File, protodesc.ToFileDescriptorProto(anypb.File_google_protobuf_any_proto))
	set.File[0].Service[0].Method = append(set.File[0].Service[0].Method, &descriptorpb.MethodDescriptorProto{Name: proto.String("AnyEcho"), InputType: proto.String(".google.protobuf.Any"), OutputType: proto.String(".google.protobuf.Any")})
	data, _ = proto.Marshal(&set)
	os.WriteFile(filepath.Join(dir, "echo.pb"), data, 0600)
	layer, err := New(dir, configuration(data))
	if err != nil {
		t.Fatal(err)
	}
	body := `@echo fn echo(s: Session) -> void implements GRPCHandler { if(s.recv()){s.send(s.body());} } bind("/wiretest.Echo/AnyEcho","echo");`
	e, err := wire.New("grpc", layer, testutil.Compile(t, "grpc", body), nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := grpc.NewClient(testutil.Start(t, e), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithNoProxy())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	request, err := anypb.New(message(d, "inside dynamic Any"))
	if err != nil {
		t.Fatal(err)
	}
	response := new(anypb.Any)
	if err = client.Invoke(context.Background(), "/wiretest.Echo/AnyEcho", request, response); err != nil {
		t.Fatal("descriptor-driven Any:", err)
	}
	decoded := dynamicpb.NewMessage(d)
	if err = response.UnmarshalTo(decoded); err != nil || value(decoded) != "inside dynamic Any" {
		t.Fatal(value(decoded), err)
	}
	badPayload := message(d, "unknown nested field")
	badPayload.SetUnknown(protowire.AppendString(protowire.AppendTag(nil, 99, protowire.BytesType), "unknown"))
	badRequest, _ := anypb.New(badPayload)
	if err = client.Invoke(context.Background(), "/wiretest.Echo/AnyEcho", badRequest, new(anypb.Any)); status.Code(err) != codes.InvalidArgument {
		t.Fatal("unknown Any payload field accepted:", err)
	}
}
func TestResponseCardinality(t *testing.T) {
	c, d := server(t, func(body string) string {
		return strings.Replace(body, "s.send(s.body());", "s.send(s.body()); s.send(s.body());", 1)
	})
	s := stream(t, c, "Unary", false, true)
	send(t, s, message(d, "one"))
	s.CloseSend()
	recv(t, s, d, "one")
	if err := s.RecvMsg(dynamicpb.NewMessage(d)); status.Code(err) != codes.Internal {
		t.Fatal("extra unary response accepted:", err)
	}
}

func TestJSONDescriptorAndStrictOptions(t *testing.T) {
	dir := t.TempDir()
	data, err := os.ReadFile("../../../examples/wire/echo-descriptor.json")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "echo.json"), data, 0600)
	hash := sha256.Sum256(data)
	options := Options{DescriptorSet: "echo.json", SHA256: hex.EncodeToString(hash[:]), Encoding: "json"}
	raw, _ := json.Marshal(options)
	if _, err = New(dir, raw); err != nil {
		t.Fatal(err)
	}
	options.Encoding = "guess"
	raw, _ = json.Marshal(options)
	if _, err = New(dir, raw); err == nil {
		t.Fatal("unknown descriptor encoding accepted")
	}
	if _, err = New(dir, []byte(`{"delay_ms":1}`)); err == nil {
		t.Fatal("timing option accepted")
	}
}

func TestInvalidUnaryDoesNotDispatch(t *testing.T) {
	c, d := server(t, func(body string) string {
		return strings.Replace(body, "if (s.recv()) { s.send(s.body()); }", `s.send("{\"text\":\"must not dispatch\"}");`, 1)
	})
	s := stream(t, c, "Unary", true, true)
	bad := message(d, "bad")
	bad.SetUnknown(protowire.AppendString(protowire.AppendTag(nil, 99, protowire.BytesType), "unknown"))
	send(t, s, bad)
	s.CloseSend()
	if err := s.RecvMsg(dynamicpb.NewMessage(d)); status.Code(err) != codes.InvalidArgument {
		t.Fatal("invalid request reached handler:", err)
	}
}

type completionStream struct{ reads int }

func (*completionStream) SetHeader(metadata.MD) error  { return nil }
func (*completionStream) SendHeader(metadata.MD) error { return nil }
func (*completionStream) SetTrailer(metadata.MD)       {}
func (*completionStream) Context() context.Context     { return context.Background() }
func (*completionStream) SendMsg(any) error            { return nil }
func (s *completionStream) RecvMsg(any) error          { s.reads++; return io.EOF }
func TestStreamCompletionDoesNotWaitForPeerHalfClose(t *testing.T) {
	dir, data, _ := descriptor(t)
	layer, err := New(dir, configuration(data))
	if err != nil {
		t.Fatal(err)
	}
	stream := &completionStream{}
	s := &session{stream: stream, method: layer.(*Layer).methods["/wiretest.Echo/Bidi"], types: layer.(*Layer).types, sent: 2}
	if err = s.complete(); err != nil {
		t.Fatal(err)
	}
	if stream.reads != 0 {
		t.Fatal("completion read pending input instead of ending the response stream")
	}
}
