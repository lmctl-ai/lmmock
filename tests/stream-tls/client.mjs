// Fixed HTTPS authority, local connection routing, real Node certificate checks.
import assert from 'node:assert/strict';
import https from 'node:https';
import fs from 'node:fs';
import {spawnSync} from 'node:child_process';

const base=new URL(process.env.LMMOCK_URL);
assert.equal(base.protocol,'https:');
const agent=new https.Agent({keepAlive:false});
const connect=agent.createConnection.bind(agent);
agent.createConnection=(options,callback)=>connect({...options,host:'127.0.0.1',port:Number(base.port),servername:'fixed.mock.test'},callback);
const chunks=await new Promise((resolve,reject)=>{
 const got=[];
 const request=https.get('https://fixed.mock.test/tokens',{agent},response=>{
  assert.equal(response.statusCode,200);
  assert.equal(response.headers['transfer-encoding'],'chunked');
  assert.equal(response.headers['content-length'],undefined);
  response.on('data',chunk=>got.push(chunk.toString('utf8')));
  response.on('end',()=>resolve(got));response.on('error',reject);
 });request.on('error',reject);
});
assert.deepEqual(chunks,['data: one\n\n','data: two\n\n'],'tokens arrived as one buffered response');
if(process.env.LMMOCK_UNTRUSTED_CONTROL==='1')throw Error('HTTPS passed without exported CA');
assert.ok(fs.existsSync(process.env.LMMOCK_CA_CERT));
for(const key of ['SSL_CERT_FILE','NODE_EXTRA_CA_CERTS','REQUESTS_CA_BUNDLE','CURL_CA_BUNDLE'])assert.equal(process.env[key],process.env.LMMOCK_CA_CERT);
fs.writeFileSync(process.env.LMMOCK_TEST_RESULT,JSON.stringify({url:base.href,ca:process.env.LMMOCK_CA_CERT}));
// A fresh Node process is essential: NODE_EXTRA_CA_CERTS is read at startup.
const clean={...process.env,LMMOCK_UNTRUSTED_CONTROL:'1'};
for(const key of ['SSL_CERT_FILE','NODE_EXTRA_CA_CERTS','REQUESTS_CA_BUNDLE','CURL_CA_BUNDLE','LMMOCK_CA_CERT'])delete clean[key];
const negative=spawnSync(process.execPath,[import.meta.filename],{env:clean,encoding:'utf8'});
assert.notEqual(negative.status,0,'untrusted HTTPS unexpectedly passed');
assert.match(negative.stderr,/SELF_SIGNED_CERT_IN_CHAIN|UNABLE_TO_VERIFY_LEAF_SIGNATURE|UNABLE_TO_GET_ISSUER_CERT_LOCALLY/,'negative control failed for an unrelated reason');
console.log('PASS fixed HTTPS + exported CA + incremental SSE; untrusted client refused');
