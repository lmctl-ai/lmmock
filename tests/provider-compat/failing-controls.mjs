// Failing controls per dialect:
// 1. malformed usage
// 2. bad event ordering
// 3. wrong content type
// 4. missing final marker
// 5. wrong auth / secret redaction
import assert from 'node:assert/strict';
import fs from 'node:fs';

const root = process.env.LMMOCK_URL;
assert.ok(root, 'Must run inside lmmock exec');
const dialect = process.env.DIALECT;
assert.ok(dialect, 'DIALECT env var must be set');
const configPath = process.env.MOCK_CONFIG || `fixtures/${dialect}/wire.json`;

const original = JSON.parse(fs.readFileSync(configPath, 'utf8'));
function set(selected, patch = {}) {
  fs.writeFileSync(configPath, JSON.stringify({ ...original, selected, ...patch }));
}

console.log(`Running failing controls for dialect: ${dialect}`);

if (dialect === 'anthropic') {
  // 1. malformed usage control
  set('bad-usage');
  const resUsage = await fetch(`${root}/v1/messages`, {
    method: 'POST',
    headers: { 'x-api-key': 'fixture-anthropic-key', 'anthropic-version': '2023-06-01', 'content-type': 'application/json' },
    body: JSON.stringify({ model: 'fixture-claude', messages: [{ role: 'user', content: 'ping' }] })
  });
  const jsonUsage = await resUsage.json();
  assert.equal(typeof jsonUsage.usage.input_tokens, 'string', 'usage.input_tokens must be malformed string');
  assert.throws(() => {
    if (typeof jsonUsage.usage.input_tokens !== 'number' || !Number.isInteger(jsonUsage.usage.input_tokens)) {
      throw new TypeError(`Invalid integer input_tokens: ${jsonUsage.usage.input_tokens}`);
    }
  }, /Invalid integer input_tokens/, 'malformed usage must be rejected by integer validation');
  console.log('PASS anthropic malformed usage control');

  // 2. bad event ordering control
  set('bad-event-ordering');
  const resOrder = await fetch(`${root}/v1/messages`, {
    method: 'POST',
    headers: { 'x-api-key': 'fixture-anthropic-key', 'anthropic-version': '2023-06-01', 'content-type': 'application/json' },
    body: JSON.stringify({ model: 'fixture-claude', messages: [{ role: 'user', content: 'ping' }], stream: true })
  });
  const textOrder = await resOrder.text();
  const stopIndex = textOrder.indexOf('content_block_stop');
  const startIndex = textOrder.indexOf('message_start');
  assert.ok(stopIndex >= 0 && startIndex >= 0 && stopIndex < startIndex, 'content_block_stop arrived before message_start');
  assert.throws(() => {
    if (stopIndex < startIndex) throw new Error('Protocol violation: stop before start');
  }, /Protocol violation: stop before start/, 'bad event ordering caught');
  console.log('PASS anthropic bad event ordering control');

  // 3. wrong content type control
  set('wrong-content-type');
  const resCt = await fetch(`${root}/v1/messages`, {
    method: 'POST',
    headers: { 'x-api-key': 'fixture-anthropic-key', 'anthropic-version': '2023-06-01', 'content-type': 'application/json' },
    body: JSON.stringify({ model: 'fixture-claude', messages: [{ role: 'user', content: 'ping' }] })
  });
  const ct = resCt.headers.get('content-type') || '';
  assert.equal(ct, 'text/plain');
  assert.throws(() => {
    if (!ct.includes('application/json')) throw new Error(`Wrong content type: ${ct}`);
  }, /Wrong content type: text\/plain/, 'wrong content type caught');
  console.log('PASS anthropic wrong content type control');

  // 4. missing final marker control
  set('missing-terminal');
  const resTerm = await fetch(`${root}/v1/messages`, {
    method: 'POST',
    headers: { 'x-api-key': 'fixture-anthropic-key', 'anthropic-version': '2023-06-01', 'content-type': 'application/json' },
    body: JSON.stringify({ model: 'fixture-claude', messages: [{ role: 'user', content: 'ping' }], stream: true })
  });
  const textTerm = await resTerm.text();
  assert.ok(!textTerm.includes('message_stop'), 'stream must lack message_stop terminal event');
  assert.throws(() => {
    if (!textTerm.includes('message_stop')) throw new Error('Stream truncated: missing message_stop');
  }, /missing message_stop/, 'missing terminal caught');
  console.log('PASS anthropic missing terminal control');

  // 5. wrong auth / secret redaction control
  set('success');
  const secretKey = 'sk-ant-secret-synthetic-token-99999';
  const resAuth = await fetch(`${root}/v1/messages`, {
    method: 'POST',
    headers: { 'x-api-key': secretKey, 'anthropic-version': '2023-06-01', 'content-type': 'application/json' },
    body: JSON.stringify({ model: 'fixture-claude', messages: [{ role: 'user', content: 'ping' }] })
  });
  assert.equal(resAuth.status, 401);
  const textAuth = await resAuth.text();
  assert.ok(!textAuth.includes(secretKey), 'error response must NEVER leak secret key');
  const jsonAuth = JSON.parse(textAuth);
  assert.equal(jsonAuth.error.type, 'authentication_error');
  console.log('PASS anthropic wrong auth & secret redaction control');

} else if (dialect === 'gemini') {
  // 1. malformed usage control
  set('bad-usage');
  const resUsage = await fetch(`${root}/v1beta/models/fixture-gemini:generateContent`, {
    method: 'POST',
    headers: { 'x-goog-api-key': 'fixture-gemini-key', 'content-type': 'application/json' },
    body: JSON.stringify({ contents: [{ parts: [{ text: 'ping' }] }] })
  });
  const jsonUsage = await resUsage.json();
  assert.equal(typeof jsonUsage.usageMetadata.promptTokenCount, 'string', 'promptTokenCount must be malformed string');
  assert.throws(() => {
    if (typeof jsonUsage.usageMetadata.promptTokenCount !== 'number') {
      throw new TypeError(`Invalid integer promptTokenCount: ${jsonUsage.usageMetadata.promptTokenCount}`);
    }
  }, /Invalid integer promptTokenCount/, 'malformed usage must be rejected');
  console.log('PASS gemini malformed usage control');

  // 2. bad event ordering control
  set('bad-event-ordering');
  const resOrder = await fetch(`${root}/v1beta/models/fixture-gemini:streamGenerateContent`, {
    method: 'POST',
    headers: { 'x-goog-api-key': 'fixture-gemini-key', 'content-type': 'application/json' },
    body: JSON.stringify({ contents: [{ parts: [{ text: 'ping' }] }] })
  });
  const textOrder = await resOrder.text();
  const stopIndex = textOrder.indexOf('finishReason');
  const textIndex = textOrder.indexOf('"text":"pong"');
  assert.ok(stopIndex >= 0 && textIndex >= 0 && stopIndex < textIndex, 'finishReason arrived before text content');
  assert.throws(() => {
    if (stopIndex < textIndex) throw new Error('Protocol violation: finishReason before content');
  }, /finishReason before content/, 'bad event ordering caught');
  console.log('PASS gemini bad event ordering control');

  // 3. wrong content type control
  set('wrong-content-type');
  const resCt = await fetch(`${root}/v1beta/models/fixture-gemini:generateContent`, {
    method: 'POST',
    headers: { 'x-goog-api-key': 'fixture-gemini-key', 'content-type': 'application/json' },
    body: JSON.stringify({ contents: [{ parts: [{ text: 'ping' }] }] })
  });
  const ct = resCt.headers.get('content-type') || '';
  assert.equal(ct, 'text/plain');
  assert.throws(() => {
    if (!ct.includes('application/json')) throw new Error(`Wrong content type: ${ct}`);
  }, /Wrong content type: text\/plain/, 'wrong content type caught');
  console.log('PASS gemini wrong content type control');

  // 4. missing final marker control
  set('missing-terminal');
  const resTerm = await fetch(`${root}/v1beta/models/fixture-gemini:streamGenerateContent`, {
    method: 'POST',
    headers: { 'x-goog-api-key': 'fixture-gemini-key', 'content-type': 'application/json' },
    body: JSON.stringify({ contents: [{ parts: [{ text: 'ping' }] }] })
  });
  const textTerm = await resTerm.text();
  assert.ok(!textTerm.includes('finishReason'), 'stream must lack finishReason terminal marker');
  assert.throws(() => {
    if (!textTerm.includes('finishReason')) throw new Error('Stream truncated: missing finishReason');
  }, /missing finishReason/, 'missing terminal caught');
  console.log('PASS gemini missing terminal control');

  // 5. wrong auth / secret redaction control
  set('success');
  const secretKey = 'goog-secret-key-xyz-8888';
  const resAuth = await fetch(`${root}/v1beta/models/fixture-gemini:generateContent`, {
    method: 'POST',
    headers: { 'x-goog-api-key': secretKey, 'content-type': 'application/json' },
    body: JSON.stringify({ contents: [{ parts: [{ text: 'ping' }] }] })
  });
  assert.equal(resAuth.status, 403);
  const textAuth = await resAuth.text();
  assert.ok(!textAuth.includes(secretKey), 'error response must NEVER leak secret key');
  const jsonAuth = JSON.parse(textAuth);
  assert.equal(jsonAuth.error.status, 'PERMISSION_DENIED');
  console.log('PASS gemini wrong auth & secret redaction control');

} else if (dialect === 'ollama') {
  // 1. malformed usage control
  set('bad-usage');
  const resUsage = await fetch(`${root}/api/chat`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ model: 'fixture-ollama', messages: [{ role: 'user', content: 'ping' }], stream: false })
  });
  const jsonUsage = await resUsage.json();
  assert.equal(typeof jsonUsage.prompt_eval_count, 'string', 'prompt_eval_count must be malformed string');
  assert.throws(() => {
    if (typeof jsonUsage.prompt_eval_count !== 'number') {
      throw new TypeError(`Invalid integer prompt_eval_count: ${jsonUsage.prompt_eval_count}`);
    }
  }, /Invalid integer prompt_eval_count/, 'malformed usage must be rejected');
  console.log('PASS ollama malformed usage control');

  // 2. bad event ordering control
  set('bad-event-ordering');
  const resOrder = await fetch(`${root}/api/chat`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ model: 'fixture-ollama', messages: [{ role: 'user', content: 'ping' }], stream: true })
  });
  const textOrder = await resOrder.text();
  const doneTrueIndex = textOrder.indexOf('"done":true');
  const doneFalseIndex = textOrder.indexOf('"done":false');
  assert.ok(doneTrueIndex >= 0 && doneFalseIndex >= 0 && doneTrueIndex < doneFalseIndex, 'done:true arrived before done:false');
  assert.throws(() => {
    if (doneTrueIndex < doneFalseIndex) throw new Error('Protocol violation: done:true before done:false');
  }, /done:true before done:false/, 'bad event ordering caught');
  console.log('PASS ollama bad event ordering control');

  // 3. wrong content type control
  set('wrong-content-type');
  const resCt = await fetch(`${root}/api/chat`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ model: 'fixture-ollama', messages: [{ role: 'user', content: 'ping' }], stream: false })
  });
  const ct = resCt.headers.get('content-type') || '';
  assert.equal(ct, 'text/plain');
  assert.throws(() => {
    if (!ct.includes('application/json') && !ct.includes('application/x-ndjson')) {
      throw new Error(`Wrong content type: ${ct}`);
    }
  }, /Wrong content type: text\/plain/, 'wrong content type caught');
  console.log('PASS ollama wrong content type control');

  // 4. missing final marker control
  set('missing-terminal');
  const resTerm = await fetch(`${root}/api/chat`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ model: 'fixture-ollama', messages: [{ role: 'user', content: 'ping' }], stream: true })
  });
  const textTerm = await resTerm.text();
  assert.ok(!textTerm.includes('"done":true'), 'stream must lack terminal done:true line');
  assert.throws(() => {
    if (!textTerm.includes('"done":true')) throw new Error('NDJSON stream truncated: missing done:true');
  }, /missing done:true/, 'missing terminal caught');
  console.log('PASS ollama missing terminal control');

  // 5. wrong auth / secret redaction control (bearer mode)
  set('success', { auth_mode: 'bearer' });
  const secretKey = 'ollama-bearer-token-secret-777';
  const resAuth = await fetch(`${root}/api/chat`, {
    method: 'POST',
    headers: { 'Authorization': `Bearer ${secretKey}`, 'content-type': 'application/json' },
    body: JSON.stringify({ model: 'fixture-ollama', messages: [{ role: 'user', content: 'ping' }] })
  });
  assert.equal(resAuth.status, 401);
  const textAuth = await resAuth.text();
  assert.ok(!textAuth.includes(secretKey), 'error response must NEVER leak secret key');
  const jsonAuth = JSON.parse(textAuth);
  assert.equal(jsonAuth.error, 'unauthorized');
  console.log('PASS ollama wrong auth & secret redaction control');

} else if (dialect === 'miniplayer') {
  // 1. malformed usage control
  set('bad-usage');
  const resUsage = await fetch(`${root}/v1/chat/completions`, {
    method: 'POST',
    headers: { 'Authorization': 'Bearer fixture-miniplayer-key', 'content-type': 'application/json', 'x-mock-provider': 'deepseek' },
    body: JSON.stringify({ model: 'deepseek-chat', messages: [{ role: 'user', content: 'ping' }] })
  });
  const jsonUsage = await resUsage.json();
  assert.equal(typeof jsonUsage.usage.prompt_tokens, 'string', 'prompt_tokens must be malformed string');
  assert.throws(() => {
    if (typeof jsonUsage.usage.prompt_tokens !== 'number') {
      throw new TypeError(`Invalid integer prompt_tokens: ${jsonUsage.usage.prompt_tokens}`);
    }
  }, /Invalid integer prompt_tokens/, 'malformed usage must be rejected');
  console.log('PASS miniplayer malformed usage control');

  // 2. bad event ordering control
  set('bad-event-ordering');
  const resOrder = await fetch(`${root}/v1/chat/completions`, {
    method: 'POST',
    headers: { 'Authorization': 'Bearer fixture-miniplayer-key', 'content-type': 'application/json', 'x-mock-provider': 'deepseek' },
    body: JSON.stringify({ model: 'deepseek-chat', messages: [{ role: 'user', content: 'ping' }], stream: true })
  });
  const textOrder = await resOrder.text();
  const stopIndex = textOrder.indexOf('"finish_reason":"stop"');
  const deltaIndex = textOrder.indexOf('"content":"pong"');
  assert.ok(stopIndex >= 0 && deltaIndex >= 0 && stopIndex < deltaIndex, 'finish_reason arrived before content delta');
  assert.throws(() => {
    if (stopIndex < deltaIndex) throw new Error('Protocol violation: finish before delta');
  }, /finish before delta/, 'bad event ordering caught');
  console.log('PASS miniplayer bad event ordering control');

  // 3. wrong content type control
  set('wrong-content-type');
  const resCt = await fetch(`${root}/v1/chat/completions`, {
    method: 'POST',
    headers: { 'Authorization': 'Bearer fixture-miniplayer-key', 'content-type': 'application/json', 'x-mock-provider': 'deepseek' },
    body: JSON.stringify({ model: 'deepseek-chat', messages: [{ role: 'user', content: 'ping' }] })
  });
  const ct = resCt.headers.get('content-type') || '';
  assert.equal(ct, 'text/plain');
  assert.throws(() => {
    if (!ct.includes('application/json')) throw new Error(`Wrong content type: ${ct}`);
  }, /Wrong content type: text\/plain/, 'wrong content type caught');
  console.log('PASS miniplayer wrong content type control');

  // 4. missing final marker control
  set('missing-terminal');
  const resTerm = await fetch(`${root}/v1/chat/completions`, {
    method: 'POST',
    headers: { 'Authorization': 'Bearer fixture-miniplayer-key', 'content-type': 'application/json', 'x-mock-provider': 'deepseek' },
    body: JSON.stringify({ model: 'deepseek-chat', messages: [{ role: 'user', content: 'ping' }], stream: true })
  });
  const textTerm = await resTerm.text();
  assert.ok(!textTerm.includes('[DONE]'), 'stream must lack [DONE] terminal event');
  assert.throws(() => {
    if (!textTerm.includes('[DONE]')) throw new Error('Stream truncated: missing [DONE]');
  }, /missing \[DONE\]/, 'missing terminal caught');
  console.log('PASS miniplayer missing terminal control');

  // 5. wrong auth / secret redaction control for each provider
  for (const provider of ['deepseek', 'moonshot', 'openrouter', 'qwen', 'ollama']) {
    if (provider === 'ollama') set('auth-error', { provider });
    else set('success', { provider });
    const secretKey = `secret-token-for-${provider}-998877`;
    const resAuth = await fetch(`${root}/v1/chat/completions`, {
      method: 'POST',
      headers: { 'Authorization': `Bearer ${secretKey}`, 'content-type': 'application/json', 'x-mock-provider': provider },
      body: JSON.stringify({ model: 'fixture-miniplayer', messages: [{ role: 'user', content: 'ping' }] })
    });
    assert.equal(resAuth.status, 401, `${provider} auth status must be 401`);
    const textAuth = await resAuth.text();
    assert.ok(!textAuth.includes(secretKey), `${provider} error response must NEVER leak secret key`);
    const jsonAuth = JSON.parse(textAuth);
    if (provider === 'deepseek') assert.equal(jsonAuth.error.code, 'invalid_api_key');
    if (provider === 'moonshot') assert.equal(jsonAuth.error.code, 'auth_header_invalid');
    if (provider === 'openrouter') assert.equal(jsonAuth.error.code, 401);
    if (provider === 'qwen') assert.equal(jsonAuth.code, 'InvalidApiKey');
    if (provider === 'ollama') assert.equal(jsonAuth.error.message, 'unauthorized');
    console.log(`PASS miniplayer ${provider} wrong auth & secret redaction control`);
  }
}

// Reset configuration back to success
set('success');
console.log(`ALL 5 FAILING CONTROLS PASSED for ${dialect}`);
