import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import {fileURLToPath} from 'node:url';

const root=path.resolve(path.dirname(fileURLToPath(import.meta.url)),'../..');
const fixtureDir=path.join(root,'captures/auth');
function read(name) {
  const content=fs.readFileSync(path.join(fixtureDir,name+'.jsonl'),'utf8').trim();
  return content.split('\n').map(line=>JSON.parse(line));
}
function paths(records) { return records.map(record=>record.request.path.split('?')[0]); }
function requirePath(records,wanted) { assert.ok(paths(records).includes(wanted),`capture should include ${wanted}`); }

const github=read('github-device-flow');
requirePath(github,'/login/device/code');
requirePath(github,'/login/oauth/access_token');
requirePath(github,'/user');
requirePath(github,'/repos/fixture-org/fixture-repo');
const inspected=github.filter(record=>record.request.headers.Authorization!==undefined);
assert.ok(inspected.length>=2,'GitHub captures include authenticated REST calls');
for(const record of inspected) {
  assert.equal(record.request.headers.Authorization[0],'<redacted:credential>','lminspect redacts synthetic Bearer credentials');
  assert.equal(record.response.headers['Github-Authentication-Token-Expiration'][0],'2030-01-01 00:00:00 UTC','GitHub capture uses UTC expiration header format');
}

const oauth=read('oauth2-pkce');
requirePath(oauth,'/authorize');
requirePath(oauth,'/oauth/token');
requirePath(oauth,'/oauth/userinfo');
const oauthAuth=oauth.find(record=>record.request.path.startsWith('/oauth/userinfo'));
assert.equal(oauthAuth.request.headers.Authorization[0],'<redacted:credential>','OAuth capture redacts its fixture access token');

const npm=read('npm-web-login');
requirePath(npm,'/-/v1/login');
requirePath(npm,'/-/v1/done');
requirePath(npm,'/-/npm/v1/user');
requirePath(npm,'/-/npm/v1/tokens');
const npmAuth=npm.find(record=>record.request.path==='/-/npm/v1/user');
assert.equal(npmAuth.request.headers.Authorization[0],'<redacted:credential>','npm capture redacts its fixture access token');
for(const file of ['github-device-flow','oauth2-pkce','npm-web-login']) {
  const raw=fs.readFileSync(path.join(fixtureDir,file+'.jsonl'),'utf8');
  assert.ok(!raw.includes('gho_mock_')&&!raw.includes('oauth_mock_')&&!raw.includes('npm_mock_access_token'),`${file} capture contains no fixture token literal`);
}
console.log('PASS lminspect synthetic auth capture fixtures');
