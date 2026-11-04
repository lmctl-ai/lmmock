import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import {spawnSync} from 'node:child_process';

export async function runMutationControls({root,run}) {
  const cases=[
    {provider:'github-device-flow',scenario:'device-flow',before:'oauth_error(x, "slow_down", interval);',after:'oauth_error(x, "authorization_pending", interval);',diagnostic:'device slow_down control'},
    {provider:'oauth2-pkce',scenario:'bad-verifier',before:'!secure_equal(sha256_url(verifier), text(rec, "challenge", ""))',after:'false',diagnostic:'PKCE mismatch must be rejected'},
    {provider:'npm-web-login',scenario:'pending',before:'x.status(202);',after:'x.status(200);',diagnostic:'npm web-login pending control'},
  ];
  for(const item of cases) {
    const dir=path.join(root,'examples/auth',item.provider);
    const source=path.join(dir,`server.mutant-${process.pid}.lms`);
    const compiled=path.join(root,'build/auth',`mutant-${item.provider}.lmcode`);
    const original=fs.readFileSync(path.join(dir,'server.lms'),'utf8');
    assert.equal(original.split(item.before).length-1,1,`${item.provider} mutation anchor must remain unique`);
    try {
      fs.writeFileSync(source,original.replace(item.before,item.after));
      const compilation=spawnSync(process.execPath,[path.join(root,'scripts/capture-fixture.mjs'),source,compiled],{cwd:root,encoding:'utf8'});
      if(compilation.error) throw compilation.error;
      assert.equal(compilation.status,0,`${item.provider} mutant compiles\n${compilation.stdout}\n${compilation.stderr}`);
      const result=run(item.provider,item.scenario,compiled);
      assert.notEqual(result.status,0,`${item.provider} test unexpectedly accepted a deliberate bug`);
      assert.ok((result.stdout+result.stderr).includes(item.diagnostic),`${item.provider} mutation did not trip the target assertion\n${result.stdout}\n${result.stderr}`);
      console.log(`PASS mutation control caught ${item.provider}: ${item.diagnostic}`);
    } finally {
      if(fs.existsSync(source)) fs.rmSync(source);
      if(fs.existsSync(compiled)) fs.rmSync(compiled);
    }
  }
}
