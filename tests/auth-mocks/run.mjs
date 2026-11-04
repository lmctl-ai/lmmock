import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {resolveLmscriptCommand} from '../provider-compat/lmscript-command.mjs';
import {runMutationControls} from './mutations.mjs';

const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..');
const binary=path.resolve(process.argv[2]||'build/lmmock');
const client=path.join(root,'tests/auth-mocks/client.mjs');

function ensureCompiled(provider) {
  const out=path.join(root,'build/auth',provider+'.lmcode');
  if(!fs.existsSync(out)) {
    fs.mkdirSync(path.dirname(out),{recursive:true});
    const src=path.join(root,'examples/auth',provider,'server.lms');
    const comp=spawnSync(process.execPath,[path.join(root,'scripts/capture-fixture.mjs'),src,out],{cwd:root,encoding:'utf8'});
    if(comp.error) throw comp.error;
    assert.equal(comp.status,0,`compile ${provider} failed\n${comp.stdout}\n${comp.stderr}`);
  }
  return out;
}

function run(provider,scenario,compiled=ensureCompiled(provider)) {
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

for(const scenario of ['json-format','form-format','naive-device-flow','fine-grained-pat','device-flow','expired','denied']) pass('github-device-flow',scenario);
for(const scenario of ['pkce','bad-verifier','client-credentials']) pass('oauth2-pkce',scenario);
for(const scenario of ['pending','web-login','unauth']) pass('npm-web-login',scenario);

// Verify direct lmscript execution of examples/auth/<provider>/server.lms (P2-5)
const cli=resolveLmscriptCommand();
const env={...process.env,PATH:`${path.dirname(binary)}${path.delimiter}${process.env.PATH||''}`};
for(const [provider,scenario] of [['github-device-flow','json-format'],['oauth2-pkce','client-credentials'],['npm-web-login','pending']]) {
  const script=path.join(root,'examples/auth',provider,'server.lms');
  const config=path.join(root,'examples/auth',provider,'config.json');
  const direct=spawnSync(cli.command,[...cli.args,script,'--config',config,'--',process.execPath,client,provider,scenario],{cwd:root,env,encoding:'utf8'});
  if(direct.error) throw direct.error;
  assert.equal(direct.status,0,`direct lmscript ${provider} failed\n${direct.stdout}\n${direct.stderr}`);
  assert.ok(direct.stdout.includes(`PASS auth mock ${provider}/${scenario}`),`direct lmscript ${provider} must execute the client command instead of exiting as a no-op`);
}

await runMutationControls({root,run});
console.log('PASS all auth provider examples and their deliberate-mutation controls');
