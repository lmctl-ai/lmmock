#!/usr/bin/env python3
"""Production-copy controls. No live source mutation, sleeps or timeout faults."""
from pathlib import Path
import os
import shutil
import subprocess
import tempfile
ROOT=Path(__file__).resolve().parents[2]
CASES=[
 ('flushed-event-lost','internal/wire/h2/h2.go','return http.NewResponseController(s.writer).Flush()','return nil','./internal/wire/h2','TestFlushBoundary'),
 ('response-order-swapped','internal/wire/grpcwire/grpc_test.go',None,None,'./internal/wire/grpcwire','TestDescriptorDrivenClients'),
 ('descriptor-pin-ignored','internal/wire/grpcwire/grpc.go','if !bytes.Equal(expected, digest[:]) {','if false && !bytes.Equal(expected, digest[:]) {','./internal/wire/grpcwire','TestDescriptorRefusalsBeforeListen'),
 ('request-cardinality-ignored','internal/wire/grpcwire/grpc.go','if !descriptor.IsStreamingClient() {','if false {','./internal/wire/grpcwire','TestWrongOrderAndDescriptorMismatchFail'),
 ('response-cardinality-ignored','internal/wire/grpcwire/grpc.go','if !s.method.IsStreamingServer() && s.sent != 0 {','if false {','./internal/wire/grpcwire','TestResponseCardinality'),
 ('schema-defaults-omitted','internal/wire/grpcwire/grpc.go','EmitDefaultValues: true','EmitDefaultValues: false','./internal/wire/grpcwire','TestProtoDefaultsAndDynamicAny'),
]
with tempfile.TemporaryDirectory(prefix='wire-controls-') as temp:
 root=Path(temp)
 for file in ('go.mod','go.sum') :shutil.copyfile(ROOT/file,root/file)
 shutil.copytree(ROOT/'internal/wire',root/'internal/wire')
 (root/'internal/proto').mkdir(parents=True);shutil.copyfile(ROOT/'internal/proto/registry.go',root/'internal/proto/registry.go')
 shutil.copytree(ROOT/'examples/wire',root/'examples/wire')
 def run(package,pattern):
  return subprocess.run(['go','test','-count=1',package,'-run','^'+pattern+'$'],cwd=root,env=os.environ,capture_output=True,text=True)
 for name,file,old,new,package,pattern in CASES:
  baseline=run(package,pattern)
  assert baseline.returncode==0,(name,'baseline',baseline.stdout,baseline.stderr)
  path=root/file;original=path.read_text()
  if name=='response-order-swapped':
   path=root/'examples/wire/grpc.lm';original=path.read_text()
   assert original.count(r's.send("{\"text\":\"first\"}"); s.send("{\"text\":\"last\"}");')==1
   mutated=original.replace(r's.send("{\"text\":\"first\"}"); s.send("{\"text\":\"last\"}");',r's.send("{\"text\":\"last\"}"); s.send("{\"text\":\"first\"}");')
  else:
   assert original.count(old)==1,(name,'stale anchor');mutated=original.replace(old,new)
  path.write_text(mutated)
  try:
   bad=run(package,pattern)
   assert bad.returncode!=0 and '--- FAIL: '+pattern in bad.stdout and '[build failed]' not in bad.stdout,(name,'not an assertion failure',bad.stdout,bad.stderr)
   print('CAUGHT',name,flush=True)
  finally:path.write_text(original)
print('PASS 6 protocol production controls')
