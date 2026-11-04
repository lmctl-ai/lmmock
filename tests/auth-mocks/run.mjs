import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {runMutationControls} from './mutations.mjs';

const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..');
const binary=path.resolve(process.argv[2]||'build/lmmock');
const client=path.join(root,'tests/auth-mocks/client.mjs');
function run(provider,scenario,compiled=path.join(root,'build/auth',provider+'.lmcode')) {
  const config=path.join(root,'examples/auth',provider,'config.json');
  const result=spawnSync(binary,['exec','--lmcode',path.resolve(compiled),'--config',config,'--',process.execPath,client,provider,scenario],{cwd:root,encoding:'utf8'});
  if(result.error) throw result.error;
  return result;
}
function pass(provider,scenario) {
  const result=run(provider,scenario);
  assert.equal(result.status,0,`${provider}/${scenario} failed\n${result.stdout}\n${result.stderr}`);
  process.stdout.write(result.stdout);
}

for(const scenario of ['json-format','form-format','device-flow','expired','denied']) pass('github-device-flow',scenario);
for(const scenario of ['pkce','bad-verifier','client-credentials']) pass('oauth2-pkce',scenario);
for(const scenario of ['pending','web-login','unauth']) pass('npm-web-login',scenario);
await runMutationControls({root,run});
console.log('PASS all auth provider examples and their deliberate-mutation controls');
