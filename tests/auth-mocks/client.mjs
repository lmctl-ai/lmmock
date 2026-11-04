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
async function githubPoll(code, overrides={}) {
  return postForm('/login/oauth/access_token',{
    client_id:'fixture-github-app-client',
    device_code:code.device_code,
    grant_type:'urn:ietf:params:oauth:grant-type:device_code',
    ...overrides,
  },{'content-type':'application/x-www-form-urlencoded',accept:'application/json'});
}
async function github(scenario) {
  if(scenario==='json-format') { await githubCode('json'); return; }
  if(scenario==='form-format') { await githubCode('form'); return; }
  if(scenario==='naive-device-flow') {
    // P1-1: Naive RFC 8628 client approves and polls without calling /__mock/clock
    const code=await githubCode();
    // P2-4: verification_uri (without ?user_code=) includes a usable user_code input value
    const barePage=await (await fetch(code.verification_uri)).text();
    assert.ok(barePage.includes(`value='${code.user_code}'`),'verification_uri page pre-populates active user_code');
    assert.equal((await postForm('/__mock/approve',{user_code:code.user_code,decision:'approve'})).status,200,'approval succeeds');
    const result=await responseJson(await githubPoll(code));
    assert.equal(result.status,200,'naive poll HTTP status is 200');
    assert.ok(result.body.access_token?.startsWith('gho_mock_'),'naive RFC 8628 client receives token without /__mock/clock');
    // P3-1: Re-polling an already-redeemed device_code must not mint a second token
    const replay=await responseJson(await githubPoll(code));
    assert.equal(replay.status,200,'replayed device_code returns HTTP 200 error');
    assert.equal(replay.body.error,'incorrect_device_code','replayed device_code is rejected');
    return;
  }
  if(scenario==='fine-grained-pat') {
    // P1-2: Fine-grained PAT omits X-OAuth-Scopes and supports public repository inspection for lmauth
    const fgHeaders={authorization:'Bearer github_pat_mock_fine_grained_token',accept:'application/vnd.github+json'};
    const user=await fetch(base+'/user',{headers:fgHeaders});
    assert.equal(user.status,200,'fine-grained PAT authenticates at /user');
    assert.equal(user.headers.get('x-oauth-scopes'),null,'fine-grained PAT omits X-OAuth-Scopes');
    assert.equal(user.headers.get('x-accepted-oauth-scopes'),null,'fine-grained PAT omits X-Accepted-OAuth-Scopes');
    assert.equal(user.headers.get('github-authentication-token-expiration'),'2030-01-01 00:00:00 UTC','fine-grained PAT exposes GitHub expiration timestamp format');
    assert.equal(user.headers.get('x-accepted-github-permissions'),'metadata=read','fine-grained PAT exposes permission hint');
    const pubRepo=await fetch(base+'/repos/lmctl-ai/lmpdf',{headers:fgHeaders});
    assert.equal(pubRepo.status,200,'public target repository inspection succeeds for lmauth');
    assert.equal(pubRepo.headers.get('x-oauth-scopes'),null,'fine-grained PAT omits X-OAuth-Scopes on /repos');
    const pubBody=await pubRepo.json();
    assert.equal(pubBody.full_name,'lmctl-ai/lmpdf','public repository full_name matches request');
    assert.equal(pubBody.private,false,'public repository reports private: false');
    return;
  }
  if(scenario==='device-flow') {
    // Negative checks (M04, M05, M06, M07, M08, P2-4)
    const badClient=await responseJson(await postJson('/login/device/code',{client_id:'wrong-client'}));
    assert.equal(badClient.status,401,'wrong client_id on device/code is rejected with 401');
    assert.equal(badClient.body.error,'incorrect_client_credentials','wrong client_id returns incorrect_client_credentials');
    assert.equal((await fetch(base+'/user')).status,401,'unauthenticated /user returns 401');
    assert.equal((await fetch(base+'/repos/fixture-org/fixture-repo')).status,401,'unauthenticated /repos returns 401');
    assert.equal((await postJson('/__mock/clock',{})).status,400,'empty /__mock/clock body returns 400 instead of 500');
    const xssPage=await (await fetch(base+'/login/device?user_code='+encodeURIComponent("'><script>alert(1)</script>"))).text();
    assert.ok(!xssPage.includes('<script>alert(1)</script>'),'HTML special characters in user_code are escaped');

    const code=await githubCode();
    // Concurrent pending device code receives a distinct user_code instead of overwriting WDJB-MJHT
    const code2=await responseJson(await postJson('/login/device/code',{client_id:'fixture-github-app-client',scope:'repo'}));
    assert.equal(code2.status,200,'second concurrent device code succeeds');
    assert.notEqual(code2.body.user_code,code.user_code,'concurrent pending device code gets distinct user_code');

    const badGrant=await responseJson(await githubPoll(code,{grant_type:'authorization_code'}));
    assert.equal(badGrant.status,200,'unsupported_grant_type returns HTTP 200');
    assert.equal(badGrant.body.error,'unsupported_grant_type','wrong grant_type is rejected');
    const badDevCode=await responseJson(await githubPoll({device_code:'unknown-device-code'}));
    assert.equal(badDevCode.status,200,'incorrect_device_code returns HTTP 200');
    assert.equal(badDevCode.body.error,'incorrect_device_code','unknown device_code is rejected');

    const firstClock=await responseJson(await clock(6));
    assert.equal(firstClock.body.now_ms,1006000,'test clock advances by six seconds');
    let result=await responseJson(await githubPoll(code));
    assert.equal(result.status,200,'authorization_pending returns HTTP 200');
    assert.equal(result.body.error,'authorization_pending','unapproved device stays pending');
    result=await responseJson(await githubPoll(code));
    assert.equal(result.status,200,'slow_down returns HTTP 200');
    assert.equal(result.body.error,'slow_down','device slow_down control');
    assert.equal(result.body.interval,10,'slow_down adds five seconds to the interval');
    await clock(11);
    result=await responseJson(await githubPoll(code));
    assert.equal(result.status,200,'authorization_pending returns HTTP 200');
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
    assert.equal(user.headers.get('github-authentication-token-expiration'),'2030-01-01 00:00:00 UTC','token expiration header is exposed in GitHub UTC format');
    assert.equal((await user.json()).login,'fixture-user','user profile shape is returned');
    // Also verify Authorization: token <tok> scheme supported by GitHub REST API
    const tokenSchemeUser=await fetch(base+'/user',{headers:{authorization:`token ${result.body.access_token}`,accept:'application/vnd.github+json'}});
    assert.equal(tokenSchemeUser.status,200,'GitHub token scheme header is accepted');
    const repo=await fetch(base+'/repos/fixture-org/fixture-repo',{headers});
    assert.equal(repo.status,200,'repository permission inspection succeeds');
    assert.equal(repo.headers.get('x-accepted-github-permissions'),'metadata=read','fine-grained permission hint is exposed');
    const repoBody=await repo.json();
    assert.equal(repoBody.full_name,'fixture-org/fixture-repo','repository identity is returned');
    assert.deepEqual(repoBody.permissions,{admin:false,maintain:false,push:true,triage:false,pull:true},'repository permission response is explicit');
    assert.equal((await fetch(base+'/repos/fixture-org/unknown-repo',{headers})).status,404,'unknown repository returns 404');
    return;
  }
  if(scenario==='expired') {
    const code=await githubCode(); await clock(900);
    const result=await responseJson(await githubPoll(code));
    assert.equal(result.status,200,'expired_token returns HTTP 200');
    assert.equal(result.body.error,'expired_token','expired device code is refused'); return;
  }
  if(scenario==='denied') {
    const code=await githubCode(); await clock(6);
    await postForm('/__mock/approve',{user_code:code.user_code,decision:'deny'});
    const result=await responseJson(await githubPoll(code));
    assert.equal(result.status,200,'access_denied returns HTTP 200');
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
    assert.ok(result.body.access_token.startsWith('oauth_mock_'),'client-credentials token is synthetic');
    // P2-1: RFC 6749 §2.3.1 HTTP Basic client authentication
    const basic=Buffer.from('fixture-public-client:fixture-client-secret').toString('base64');
    const basicRes=await responseJson(await postForm('/oauth/token',{
      grant_type:'client_credentials',scope:'machine:read',
    },{'content-type':'application/x-www-form-urlencoded',authorization:`Basic ${basic}`}));
    assert.equal(basicRes.status,200,'HTTP Basic client_credentials succeeds');
    assert.ok(basicRes.body.access_token.startsWith('oauth_mock_'),'HTTP Basic client_credentials returns synthetic token');
    return;
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
    assert.equal(token.body.error,'invalid_grant','PKCE mismatch returns invalid_grant');
    // P2-2: RFC 7636 §4.1 short (<43 chars) code_verifier must be rejected even when S256 matches
    const shortVerifier='short_verifier_under_43_chars';
    const shortChallenge=createHash('sha256').update(shortVerifier).digest('base64url');
    const shortAuth=await fetch(base+'/authorize?'+new URLSearchParams({
      response_type:'code',client_id:'fixture-public-client',redirect_uri:redirectUri,
      scope:'profile',state:'st-short',code_challenge:shortChallenge,code_challenge_method:'S256',
    }).toString(),{redirect:'manual'});
    const shortCode=new URL(shortAuth.headers.get('location')).searchParams.get('code');
    const shortTok=await responseJson(await postForm('/oauth/token',{
      grant_type:'authorization_code',client_id:'fixture-public-client',code:shortCode,
      redirect_uri:redirectUri,code_verifier:shortVerifier,
    }));
    assert.equal(shortTok.status,400,'RFC 7636 short code_verifier must be rejected');
    assert.equal(shortTok.body.error,'invalid_grant','short code_verifier returns invalid_grant');
    return;
  }
  assert.equal(token.status,200,'valid S256 verifier exchanges the code');
  assert.equal(token.body.token_type,'Bearer','token response declares Bearer');
  assert.equal(token.body.scope,'profile','authorization scope is preserved');
  // M10: Authorization code is single-use
  const replay=await responseJson(await postForm('/oauth/token',{
    grant_type:'authorization_code',client_id:'fixture-public-client',code,
    redirect_uri:redirectUri,code_verifier:verifier,
  }));
  assert.equal(replay.status,400,'replayed authorization code is rejected');
  assert.equal(replay.body.error,'invalid_grant','replayed authorization code returns invalid_grant');

  // M11: Mismatched redirect_uri on /oauth/token is rejected
  const auth2=await fetch(base+'/authorize?'+params.toString(),{redirect:'manual'});
  const code2=new URL(auth2.headers.get('location')).searchParams.get('code');
  const badRedirectTok=await responseJson(await postForm('/oauth/token',{
    grant_type:'authorization_code',client_id:'fixture-public-client',code:code2,
    redirect_uri:'http://127.0.0.1:45678/callback',code_verifier:verifier,
  }));
  assert.equal(badRedirectTok.status,400,'mismatched redirect_uri on token exchange is rejected');

  // P2-1: Ephemeral loopback port redirect + HTTP Basic client authentication on authorization_code
  const loopUri='http://127.0.0.1:45678/callback';
  const loopAuth=await fetch(base+'/authorize?'+new URLSearchParams({
    response_type:'code',client_id:'fixture-public-client',redirect_uri:loopUri,
    scope:'profile',state:'st-loop',code_challenge:challenge,code_challenge_method:'S256',
  }).toString(),{redirect:'manual'});
  assert.equal(loopAuth.status,302,'RFC 8252 ephemeral loopback redirect_uri is accepted');
  const loopCode=new URL(loopAuth.headers.get('location')).searchParams.get('code');
  const basic=Buffer.from('fixture-public-client:fixture-client-secret').toString('base64');
  const loopTok=await responseJson(await postForm('/oauth/token',{
    grant_type:'authorization_code',code:loopCode,redirect_uri:loopUri,code_verifier:verifier,
  },{'content-type':'application/x-www-form-urlencoded',authorization:`Basic ${basic}`}));
  assert.equal(loopTok.status,200,'authorization_code exchange with HTTP Basic auth succeeds');

  // M09 & P2-2: Unregistered redirect_uri returns 400; invalid response_type with valid redirect_uri redirects with error (RFC 6749 §4.1.2.1)
  const unregAuth=await fetch(base+'/authorize?'+new URLSearchParams({
    response_type:'code',client_id:'fixture-public-client',redirect_uri:'http://evil.example.test/callback',
    scope:'profile',state,code_challenge:challenge,code_challenge_method:'S256',
  }).toString(),{redirect:'manual'});
  assert.equal(unregAuth.status,400,'unregistered redirect_uri returns 400');
  const badRespType=await fetch(base+'/authorize?'+new URLSearchParams({
    response_type:'token',client_id:'fixture-public-client',redirect_uri:redirectUri,
    scope:'profile',state:'st-err',code_challenge:challenge,code_challenge_method:'S256',
  }).toString(),{redirect:'manual'});
  assert.equal(badRespType.status,302,'invalid response_type with valid redirect_uri redirects per RFC 6749 §4.1.2.1');
  const errLoc=new URL(badRespType.headers.get('location'));
  assert.equal(errLoc.searchParams.get('error'),'unsupported_response_type','redirect includes unsupported_response_type');
  assert.equal(errLoc.searchParams.get('state'),'st-err','redirect preserves state');

  // M12: Unauthenticated /oauth/userinfo returns 401
  assert.equal((await fetch(base+'/oauth/userinfo')).status,401,'unauthenticated userinfo returns 401');
  const user=await fetch(base+'/oauth/userinfo',{headers:{authorization:`Bearer ${token.body.access_token}`} });
  assert.equal(user.status,200,'access token reaches synthetic userinfo');
  assert.equal((await user.json()).sub,'fixture-user','userinfo response is valid');
}

async function npmLogin(scenario) {
  if(scenario==='unauth') {
    assert.equal((await fetch(base+'/-/whoami')).status,401,'npm whoami requires auth');
    assert.equal((await fetch(base+'/-/npm/v1/user')).status,401,'npm profile requires auth');
    assert.equal((await fetch(base+'/-/npm/v1/tokens')).status,401,'npm token listing requires auth');
    // P2-3: Pre-configured token in config.json works directly on /-/whoami, /-/npm/v1/user, and /-/npm/v1/tokens
    const cfgHeaders={authorization:'Bearer npm_mock_token_from_config',accept:'application/json'};
    const whoami=await responseJson(await fetch(base+'/-/whoami',{headers:cfgHeaders}));
    assert.equal(whoami.status,200,'configured token authenticates at /-/whoami before interactive login');
    assert.equal(whoami.body.username,'fixture-npm-user','/-/whoami returns configured username');
    const preUser=await responseJson(await fetch(base+'/-/npm/v1/user',{headers:cfgHeaders}));
    assert.equal(preUser.status,200,'configured token authenticates at /-/npm/v1/user before interactive login');
    return;
  }
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
    assert.equal((await fetch(base+'/login/cli/unknown-session')).status,404,'unknown login page session returns 404');
    const pending=await responseJson(await fetch(doneUrl));
    assert.equal(pending.status,202,'doneUrl reports pending before approval');
    const session=doneUrl.searchParams.get('sessionId');
    assert.equal((await postForm('/__mock/approve',{sessionId:session})).status,200,'mock browser approval event is accepted');
    assert.equal((await postForm('/__mock/approve',{sessionId:session})).status,404,'duplicate approval on non-pending session is rejected');
    const complete=await responseJson(await fetch(doneUrl));
    assert.equal(complete.status,200,'doneUrl returns after approval');
    assert.ok(complete.body.token.startsWith('npm_mock_'),'registry token is synthetic');
    const headers={authorization:`Bearer ${complete.body.token}`,accept:'application/json'};
    const whoami=await responseJson(await fetch(base+'/-/whoami',{headers}));
    assert.equal(whoami.status,200,'npm whoami authenticates the token');
    assert.equal(whoami.body.username,'fixture-npm-user','whoami returns username');
    const user=await responseJson(await fetch(base+'/-/npm/v1/user',{headers}));
    assert.equal(user.status,200,'npm profile endpoint authenticates the token');
    assert.equal(user.body.name,'fixture-npm-user','profile shape matches npm registry docs');
    const tokens=await responseJson(await fetch(base+'/-/npm/v1/tokens',{headers}));
    assert.equal(tokens.status,200,'npm token listing authenticates the token');
    assert.equal(tokens.body.objects[0].key,'0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef','token listing returns its documented SHA-512 key shape');
    assert.equal(tokens.body.objects[0].name,'fixture-cli-token','token listing includes token name');
    assert.equal(tokens.body.objects[0].token,'[REDACTED]','token listing does not repeat the credential');
    assert.equal(tokens.body.objects[0].readonly,false,'token listing reports readonly: false');
    assert.equal(tokens.body.objects[0].automation,false,'token listing reports automation: false');
    assert.equal(tokens.body.objects[0].cidr_whitelist,null,'token listing reports cidr_whitelist: null');
    assert.equal((await responseJson(await fetch(doneUrl))).status,404,'completed doneUrl does not replay the token');
    return;
  }
  throw Error(`unknown npm web-login case ${scenario}`);
}

if(provider==='github-device-flow') await github(scenario);
else if(provider==='oauth2-pkce') await oauth2(scenario);
else if(provider==='npm-web-login') await npmLogin(scenario);
else throw Error(`unknown auth example ${provider}`);
console.log(`PASS auth mock ${provider}/${scenario}`);
