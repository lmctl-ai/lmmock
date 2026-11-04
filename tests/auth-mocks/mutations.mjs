import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import {spawnSync} from 'node:child_process';

export async function runMutationControls({root,run}) {
  const cases=[
    {provider:'github-device-flow',scenario:'device-flow',before:'oauth_error(x, "slow_down", interval);',after:'oauth_error(x, "authorization_pending", interval);',diagnostic:'device slow_down control'},
    {provider:'github-device-flow',scenario:'expired',before:'oauth_error(x, "expired_token", 0);',after:'x.status(400); oauth_error(x, "expired_token", 0);',diagnostic:'expired_token returns HTTP 200'},
    {provider:'github-device-flow',scenario:'device-flow',before:'x.set_header("X-OAuth-Scopes", text(rec, "scope", ""));',after:'x.set_header("X-Unused-Scopes", text(rec, "scope", ""));',diagnostic:'OAuth scopes header is exposed'},
    {provider:'github-device-flow',scenario:'fine-grained-pat',before:'if (text(rec, "kind", "oauth") != "fine-grained" && !flag(c, "omitOAuthScopes", false)) {',after:'if (true) {',diagnostic:'fine-grained PAT omits X-OAuth-Scopes'},
    {provider:'github-device-flow',scenario:'naive-device-flow',before:'let claimed = x.take("device:" + code);',after:'let claimed = x.get("device:" + code);',diagnostic:'replayed device_code is rejected'},
    {provider:'github-device-flow',scenario:'device-flow',before:'if (client == "" || client != text(c, "clientId", "fixture-github-app-client")) {',after:'if (false) {',diagnostic:'wrong client_id on device/code is rejected with 401'},
    {provider:'github-device-flow',scenario:'device-flow',before:'if (field(x, "grant_type") != "urn:ietf:params:oauth:grant-type:device_code") {',after:'if (false) {',diagnostic:'wrong grant_type is rejected'},
    {provider:'github-device-flow',scenario:'device-flow',before:'else if (recText == "") { oauth_error(x, "incorrect_device_code", 0); }',after:'else if (recText == "") { oauth_error(x, "authorization_pending", 0); }',diagnostic:'unknown device_code is rejected'},
    {provider:'github-device-flow',scenario:'device-flow',before:'else { x.status(404); x.json("{\\"message\\":\\"Not Found\\"}"); }',after:'else { x.status(200); x.json("{\\"message\\":\\"Not Found\\"}"); }',diagnostic:'unknown repository returns 404'},
    {provider:'oauth2-pkce',scenario:'bad-verifier',before:'!secure_equal(sha256_url(verifier), text(rec, "challenge", ""))',after:'false',diagnostic:'PKCE mismatch must be rejected'},
    {provider:'oauth2-pkce',scenario:'bad-verifier',before:'!valid_verifier(verifier) || ',after:'',diagnostic:'RFC 7636 short code_verifier must be rejected'},
    {provider:'oauth2-pkce',scenario:'pkce',before:'let code = field(x, "code"); let data = x.take("code:" + code);',after:'let code = field(x, "code"); let data = x.get("code:" + code);',diagnostic:'replayed authorization code is rejected'},
    {provider:'oauth2-pkce',scenario:'pkce',before:'if (hasBasic) { id = basic_id(x.header("Authorization")); secret = basic_secret(x.header("Authorization")); }',after:'if (false) { id = basic_id(x.header("Authorization")); }',diagnostic:'authorization_code exchange with HTTP Basic auth succeeds'},
    {provider:'npm-web-login',scenario:'pending',before:'x.status(202);',after:'x.status(200);',diagnostic:'npm web-login pending control'},
    {provider:'npm-web-login',scenario:'web-login',before:'else if (status == "approved" && x.take("approved:" + session) != "") {',after:'else if (status != "pending") {',diagnostic:'completed doneUrl does not replay the token'},
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
