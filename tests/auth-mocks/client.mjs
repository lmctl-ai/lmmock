import assert from 'node:assert/strict';
import {createHash, randomBytes} from 'node:crypto';

const [provider, scenario] = process.argv.slice(2);
const base = process.env.LMMOCK_URL;
assert.ok(base, 'lmmock must provide LMMOCK_URL');
const jsonHeaders = {'content-type':'application/json',accept:'application/json'};
const responseJson = async (response) => ({status:response.status,body:await response.json(),headers:response.headers});
const postJson = (path,body,headers=jsonHeaders) => fetch(base+path,{method:'POST',headers,body:JSON.stringify(body)});
const postForm = (path,body,headers={'content-type':'application/x-www-form-urlencoded'}) => fetch(base+path,{method:'POST',headers,body:new URLSearchParams(body)});
const clock = (seconds) => postJson('/__mock/clock',{advance_seconds:seconds});

async function githubCode(format='json') {
  const fields={client_id:'fixture-github-app-client',scope:'repo, workflow'};
  if(format==='json') {
    const result=await responseJson(await postJson('/login/device/code',fields));
    assert.equal(result.status,200,'device-code JSON request succeeds');
    assert.equal(typeof result.body.device_code,'string','device_code is returned');
    assert.equal(result.body.user_code,'WDJB-MJHT','fixture user code is stable');
    assert.equal(result.body.verification_uri,new URL('/login/device',base).href,'verification URI points to the mock page');
    assert.equal(result.body.expires_in,900,'device expiry is expressed in seconds');
    assert.equal(result.body.interval,5,'initial poll interval is expressed in seconds');
    return result.body;
  }
  const response=await postForm('/login/device/code',fields,{'content-type':'application/x-www-form-urlencoded',accept:'application/x-www-form-urlencoded'});
  assert.equal(response.status,200,'device-code form request succeeds');
  assert.equal(response.headers.get('content-type'),'application/x-www-form-urlencoded','form response content type follows Accept');
  const body=Object.fromEntries(new URLSearchParams(await response.text()));
  assert.equal(body.user_code,'WDJB-MJHT','form response includes user_code');
  assert.equal(body.verification_uri,new URL('/login/device',base).href,'form response includes verification_uri');
  assert.equal(body.expires_in,'900','form response includes expires_in');
  assert.equal(body.interval,'5','form response includes interval');
  return body;
}
async function githubPoll(code) {
  return postForm('/login/oauth/access_token',{
    client_id:'fixture-github-app-client',
    device_code:code.device_code,
    grant_type:'urn:ietf:params:oauth:grant-type:device_code',
  },{'content-type':'application/x-www-form-urlencoded',accept:'application/json'});
}
async function github(scenario) {
  if(scenario==='json-format') { await githubCode('json'); return; }
  if(scenario==='form-format') { await githubCode('form'); return; }
  if(scenario==='device-flow') {
    const code=await githubCode();
    const firstClock=await responseJson(await clock(6));
    assert.equal(firstClock.body.now_ms,1006000,'test clock advances by six seconds');
    let result=await responseJson(await githubPoll(code));
    assert.equal(result.body.error,'authorization_pending','unapproved device stays pending');
    result=await responseJson(await githubPoll(code));
    assert.equal(result.body.error,'slow_down','device slow_down control');
    assert.equal(result.body.interval,10,'slow_down adds five seconds to the interval');
    await clock(11);
    result=await responseJson(await githubPoll(code));
    assert.equal(result.body.error,'authorization_pending','device stays pending until approval');
    const page=await fetch(base+'/login/device?user_code='+encodeURIComponent(code.user_code));
    assert.equal(page.status,200,'mock approval page is available');
    assert.ok((await page.text()).includes(code.user_code),'approval page displays the user code');
    assert.equal((await postForm('/__mock/approve',{user_code:code.user_code,decision:'approve'})).status,200,'mock browser approval event is accepted');
    await clock(11);
    result=await responseJson(await githubPoll(code));
    assert.equal(result.status,200,'approved device returns a token');
    assert.equal(result.body.token_type,'bearer','device token type matches the OAuth response');
    assert.ok(result.body.access_token.startsWith('gho_mock_'),'access token is synthetic');
    assert.equal(result.body.scope,'repo, workflow','granted scope is preserved');
    const headers={authorization:`Bearer ${result.body.access_token}`,accept:'application/vnd.github+json'};
    const user=await fetch(base+'/user',{headers});
    assert.equal(user.status,200,'token introspection identifies the fixture user');
    assert.equal(user.headers.get('x-oauth-scopes'),'repo, workflow','OAuth scopes header is exposed');
    assert.equal(user.headers.get('x-accepted-oauth-scopes'),'read:user, user','accepted scopes header is exposed');
    assert.equal(user.headers.get('github-authentication-token-expiration'),'2030-01-01T00:00:00Z','token expiration header is exposed');
    assert.equal((await user.json()).login,'fixture-user','user profile shape is returned');
    const repo=await fetch(base+'/repos/fixture-org/fixture-repo',{headers});
    assert.equal(repo.status,200,'repository permission inspection succeeds');
    assert.equal(repo.headers.get('x-accepted-github-permissions'),'metadata=read','fine-grained permission hint is exposed');
    const repoBody=await repo.json();
    assert.equal(repoBody.full_name,'fixture-org/fixture-repo','repository identity is returned');
    assert.deepEqual(repoBody.permissions,{admin:false,maintain:false,push:true,triage:false,pull:true},'repository permission response is explicit');
    return;
  }
  if(scenario==='expired') {
    const code=await githubCode(); await clock(900);
    const result=await responseJson(await githubPoll(code));
    assert.equal(result.body.error,'expired_token','expired device code is refused'); return;
  }
  if(scenario==='denied') {
    const code=await githubCode(); await clock(6);
    await postForm('/__mock/approve',{user_code:code.user_code,decision:'deny'});
    const result=await responseJson(await githubPoll(code));
    assert.equal(result.body.error,'access_denied','human denial ends the device flow'); return;
  }
  throw Error(`unknown GitHub device case ${scenario}`);
}

async function oauth2(scenario) {
  if(scenario==='client-credentials') {
    const result=await responseJson(await postForm('/oauth/token',{
      grant_type:'client_credentials',client_id:'fixture-public-client',client_secret:'fixture-client-secret',scope:'machine:read',
    }));
    assert.equal(result.status,200,'plain client-credentials token endpoint succeeds');
    assert.equal(result.body.scope,'machine:read','plain token grant keeps scope');
    assert.ok(result.body.access_token.startsWith('oauth_mock_'),'client-credentials token is synthetic'); return;
  }
  const verifier=randomBytes(32).toString('base64url');
  const challenge=createHash('sha256').update(verifier).digest('base64url');
  const state='fixture-state-value'; const redirectUri='http://client.example.test/callback';
  const params=new URLSearchParams({
    response_type:'code',client_id:'fixture-public-client',redirect_uri:redirectUri,
    scope:'profile',state,code_challenge:challenge,code_challenge_method:'S256',
  });
  const auth=await fetch(base+'/authorize?'+params.toString(),{redirect:'manual'});
  assert.equal(auth.status,302,'authorization endpoint redirects to the registered callback');
  const location=new URL(auth.headers.get('location'));
  assert.equal(location.origin,new URL(redirectUri).origin,'redirect origin is allow-listed');
  assert.equal(location.pathname,new URL(redirectUri).pathname,'redirect path is allow-listed');
  assert.equal(location.searchParams.get('state'),state,'OAuth state round-trips exactly');
  const code=location.searchParams.get('code'); assert.ok(code,'authorization code is returned');
  const verifierToSend=scenario==='bad-verifier'?verifier+'x':verifier;
  const token=await responseJson(await postForm('/oauth/token',{
    grant_type:'authorization_code',client_id:'fixture-public-client',code,
    redirect_uri:redirectUri,code_verifier:verifierToSend,
  }));
  if(scenario==='bad-verifier') {
    assert.equal(token.status,400,'PKCE mismatch must be rejected');
    assert.equal(token.body.error,'invalid_grant','PKCE mismatch returns invalid_grant'); return;
  }
  assert.equal(token.status,200,'valid S256 verifier exchanges the code');
  assert.equal(token.body.token_type,'Bearer','token response declares Bearer');
  assert.equal(token.body.scope,'profile','authorization scope is preserved');
  const user=await fetch(base+'/oauth/userinfo',{headers:{authorization:`Bearer ${token.body.access_token}`} });
  assert.equal(user.status,200,'access token reaches synthetic userinfo');
  assert.equal((await user.json()).sub,'fixture-user','userinfo response is valid');
}

async function npmLogin(scenario) {
  const start=await responseJson(await postJson('/-/v1/login',{}, {'content-type':'application/json','npm-auth-type':'web'}));
  assert.equal(start.status,200,'npm web login initialization succeeds');
  const loginUrl=new URL(start.body.loginUrl); const doneUrl=new URL(start.body.doneUrl);
  assert.equal(loginUrl.origin,new URL(base).origin,'loginUrl is absolute on the mock origin');
  assert.equal(doneUrl.origin,new URL(base).origin,'doneUrl is absolute on the mock origin');
  if(scenario==='pending') {
    const pending=await responseJson(await fetch(doneUrl));
    assert.equal(pending.status,202,'npm web-login pending control');
    assert.equal(pending.headers.get('retry-after'),'0','pending response has deterministic Retry-After'); return;
  }
  if(scenario==='web-login') {
    const page=await fetch(loginUrl);
    assert.equal(page.status,200,'synthetic login page is available');
    assert.ok((await page.text()).includes('Fixture npm sign-in'),'login page is the mock provider page');
    const pending=await responseJson(await fetch(doneUrl));
    assert.equal(pending.status,202,'doneUrl reports pending before approval');
    const session=doneUrl.searchParams.get('sessionId');
    assert.equal((await postForm('/__mock/approve',{sessionId:session})).status,200,'mock browser approval event is accepted');
    const complete=await responseJson(await fetch(doneUrl));
    assert.equal(complete.status,200,'doneUrl returns after approval');
    assert.ok(complete.body.token.startsWith('npm_mock_'),'registry token is synthetic');
    const headers={authorization:`Bearer ${complete.body.token}`,accept:'application/json'};
    const user=await responseJson(await fetch(base+'/-/npm/v1/user',{headers}));
    assert.equal(user.status,200,'npm profile endpoint authenticates the token');
    assert.equal(user.body.name,'fixture-npm-user','profile shape matches npm registry docs');
    const tokens=await responseJson(await fetch(base+'/-/npm/v1/tokens',{headers}));
    assert.equal(tokens.status,200,'npm token listing authenticates the token');
    assert.equal(tokens.body.objects[0].key,'0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef','token listing returns its documented SHA-512 key shape');
    assert.equal(tokens.body.objects[0].token,'[REDACTED]','token listing does not repeat the credential');
    assert.equal((await responseJson(await fetch(doneUrl))).status,404,'completed doneUrl does not replay the token');
    return;
  }
  if(scenario==='unauth') {
    assert.equal((await fetch(base+'/-/npm/v1/user')).status,401,'npm profile requires auth');
    assert.equal((await fetch(base+'/-/npm/v1/tokens')).status,401,'npm token listing requires auth'); return;
  }
  throw Error(`unknown npm web-login case ${scenario}`);
}

if(provider==='github-device-flow') await github(scenario);
else if(provider==='oauth2-pkce') await oauth2(scenario);
else if(provider==='npm-web-login') await npmLogin(scenario);
else throw Error(`unknown auth example ${provider}`);
console.log(`PASS auth mock ${provider}/${scenario}`);
