#!/usr/bin/env python3
"""A broken production scrub rule must fail an assertion, not compilation."""
import pathlib,shutil,subprocess,tempfile
root=pathlib.Path(__file__).resolve().parents[2]
with tempfile.TemporaryDirectory(prefix='scenario-scrub-control-') as tmp:
 work=pathlib.Path(tmp)/'repo'
 shutil.copytree(root,work,ignore=shutil.ignore_patterns('.git','build','node_modules'))
 def run():return subprocess.run(['go','test','./internal/scenario','-count=1','-run','TestScrubKnownCredentials'],cwd=work,capture_output=True,text=True)
 p=run();assert p.returncode==0,p.stdout+p.stderr
 source=work/'internal/scenario/scenario.go';text=source.read_text();needle='s = strings.ReplaceAll(s, secret, "<redacted:credential>")';assert needle in text
 source.write_text(text.replace(needle,'_ = secret // deliberately omit the scrub rule'))
 p=run();assert p.returncode!=0 and '--- FAIL: TestScrubKnownCredentials' in p.stdout and 'build failed' not in p.stdout+p.stderr,p.stdout+p.stderr
 print('CAUGHT missing credential scrub rule (residue refusal; no secret emitted)')
 # SSE-order is a real replay mutation, asserted in the regular HTTP suite.
