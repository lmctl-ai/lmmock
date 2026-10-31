#!/usr/bin/env python3
"""Compiled production faults; an assertion failure is required, never a build error."""
import json,pathlib,subprocess,tempfile
root=pathlib.Path(__file__).resolve().parents[2]
faults=[
 ('buffered events','internal/mock/streaming.go','return http.NewResponseController(s.writer).Flush()','return nil','TestStreamingTokensBeforeHandlerAdvances','tokens buffered until handler completion'),
 ('missing CA export','internal/mock/tls.go','if s.CACert != "" {','if false {','TestTLSChildEnvironmentIsScoped','missing CA export'),
 ('repaired truncated chunk','internal/mock/streaming.go','buffer.WriteString("10\\r\\ncut")','buffer.WriteString("0\\r\\n\\r\\n")','TestStreamingFramingFaults/truncated-chunk','fault was buffered or repaired'),
 ('repaired malformed chunk','internal/mock/streaming.go','buffer.WriteString("not-a-chunk\\r\\n")','buffer.WriteString("0\\r\\n\\r\\n")','TestStreamingFramingFaults/malformed-chunk','fault was buffered or repaired'),
 ('missing reset','internal/mock/request_log.go','x.state.counts = map[string]int64{}','_ = x.state.counts','TestRequestLogAndResetFromLMCode','reset retained shared state/counters'),
 ('unscrubbed log body','internal/scenario/request_log.go','clean.Content, _ = n.body(body, nil)','clean.Content = request.Content','TestRequestLogAndResetFromLMCode','request log leaked a synthetic credential'),
]
for name,file,before,after,test,assertion in faults:
 baseline=subprocess.run(['go','test','./internal/mock','-run','^'+test+'$','-count=1'],cwd=root,capture_output=True,text=True)
 assert baseline.returncode==0,baseline.stdout+baseline.stderr
 with tempfile.TemporaryDirectory(prefix='lmmock-stream-control-') as temporary:
  folder=pathlib.Path(temporary);source=root/file;text=source.read_text();assert before in text,(name,'anchor missing')
  replacement=folder/source.name;replacement.write_text(text.replace(before,after))
  overlay=folder/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(source):str(replacement)}}))
  result=subprocess.run(['go','test','-overlay',str(overlay),'./internal/mock','-run','^'+test+'$','-count=1'],cwd=root,capture_output=True,text=True)
  output=result.stdout+result.stderr
  assert result.returncode==1 and '--- FAIL:' in output and assertion in output and 'build failed' not in output and '[build failed]' not in output,(name,output)
  print('CAUGHT',name,'at',assertion)

# The S8 acceptance oracle itself must reject a buffered executable, not just
# a recorder missing Flush. Keep TLS/CA verification unchanged in this mutant.
with tempfile.TemporaryDirectory(prefix='lmmock-buffered-client-') as temporary:
 folder=pathlib.Path(temporary)
 source=root/'internal/mock/streaming.go';text=source.read_text()
 needle='return http.NewResponseController(s.writer).Flush()';assert needle in text
 replacement=folder/'streaming.go';replacement.write_text(text.replace(needle,'return nil'))
 overlay=folder/'overlay.json';overlay.write_text(json.dumps({'Replace':{str(source):str(replacement)}}))
 import os
 binary=folder/('lmmock.exe' if os.name=='nt' else 'lmmock')
 compiled=subprocess.run(['go','build','-overlay',str(overlay),'-o',str(binary),'./cmd/lmmock'],cwd=root,capture_output=True,text=True)
 assert compiled.returncode==0,compiled.stdout+compiled.stderr
 result=subprocess.run(['node','tests/stream-tls/run.mjs',str(binary)],cwd=root,capture_output=True,text=True)
 output=result.stdout+result.stderr
 assert result.returncode==1 and 'AssertionError' in output and 'tokens arrived as one buffered response' in output,output
 print('CAUGHT actual buffered executable at fixed-HTTPS token-by-token arrival assertion')
