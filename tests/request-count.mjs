// usage: node tests/request-count.mjs build/lmmock   (spawns `lmmock exec` on build/llm-api.dslb and runs itself as the client)
// GET /__requests reports how many LLM requests the mock served; a retry-mode client can therefore prove the 429
// attempt really happened before the 200.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';

if (process.argv[3] !== 'client') {
  const binary = path.resolve(process.argv[2] || 'build/lmmock');
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'lmmock request count '));
  const config = path.join(dir, 'llm.json');
  fs.writeFileSync(config, JSON.stringify({ apiKey: 'fixture-key' }));
  try {
    const r = spawnSync(binary, ['exec', '--lmcode', 'build/llm-api.dslb', '--config', config, '--', process.execPath, import.meta.filename, binary, 'client'],
      { env: { ...process.env, MOCK_CONFIG: config }, encoding: 'utf8', timeout: 60000 });
    process.stdout.write(r.stdout); process.stderr.write(r.stderr);
    process.exitCode = r.status === 0 ? 0 : 1;
  } finally { fs.rmSync(dir, { recursive: true, force: true }); }
} else {
const origin = process.env.LMMOCK_URL;
assert.ok(origin, 'LMMOCK_URL is set by lmmock exec');
const config = process.env.MOCK_CONFIG;
assert.ok(config, 'MOCK_CONFIG names the config file the mock re-reads');
const set = (value) => fs.writeFileSync(config, JSON.stringify({ apiKey: 'fixture-key', ...value }));
const count = async () => (await (await fetch(origin + '/__requests')).json()).count;
const post = (body = {}) => fetch(origin + '/v1/chat/completions', {
  method: 'POST',
  headers: { Authorization: 'Bearer fixture-key', 'Content-Type': 'application/json' },
  body: JSON.stringify({ model: 'mock-agent', messages: [{ role: 'user', content: 'ping' }], ...body }),
});

assert.equal(await count(), 0, 'a fresh mock has served nothing');
set({ mode: 'retry' });
assert.equal((await post()).status, 429);
assert.equal(await count(), 1);
assert.equal((await post()).status, 200);
assert.equal(await count(), 2, 'both attempts are counted');
const inspect = await (await fetch(origin + '/__inspect')).json();
assert.equal(inspect.url, '/v1/chat/completions');
assert.equal(await count(), 2, 'reading /__requests and /__inspect is not a request');
const responses = await fetch(origin + '/v1/responses', {
  method: 'POST',
  headers: { Authorization: 'Bearer fixture-key', 'Content-Type': 'application/json' },
  body: JSON.stringify({ model: 'mock-agent', input: 'ping' }),
});
assert.equal(responses.status, 200);
assert.equal(await count(), 3, 'the Responses API is counted too');
console.log('PASS request counter');
}
