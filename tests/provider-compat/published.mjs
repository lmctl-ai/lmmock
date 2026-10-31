// Real published lmctl adapter -> pinned Miniplayer -> local lmcode HTTP mock
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';

const dialect = process.env.DIALECT || 'miniplayer';
const source = process.env.MINIPLAYER_TEST_BIN;
assert.ok(source && fs.existsSync(source), 'MINIPLAYER_TEST_BIN must name pinned real Miniplayer');
const mockUrl = process.env.LMMOCK_URL;
assert.ok(mockUrl, 'Must run inside lmmock exec');
const config = process.env.MOCK_CONFIG || `fixtures/${dialect}/wire.json`;
const original = JSON.parse(fs.readFileSync(config, 'utf8'));

const home = fs.mkdtempSync(path.join(os.tmpdir(), `lmmock-${dialect}-lmctl-`));
const quote = s => "'" + s.replaceAll("'", "'\\''") + "'";

try {
  const bin = path.join(home, 'bin');
  fs.mkdirSync(bin);
  fs.writeFileSync(path.join(bin, 'miniplayer'), '#!/bin/sh\nexec ' + [process.execPath, source].map(quote).join(' ') + ' "$@"\n', {mode: 0o700});

  const env = Object.fromEntries(
    Object.entries(process.env).filter(([k]) => !k.startsWith('LMCTL_') && !k.startsWith('LMBEE_') && !/TOKEN|API_KEY|SECRET|PASSWORD/.test(k))
  );
  Object.assign(env, {
    HOME: home,
    USERPROFILE: home,
    LMCTL_HOME: path.join(home, 'lmctl'),
    LMCTL_DB: path.join(home, 'state.db'),
    MINIPLAYER_HOME: path.join(home, 'sessions-home'),
    MINIPLAYER_CONFIG: path.join(home, 'config.json'),
    MINIPLAYER_CATALOG: path.join(home, 'catalog.json'),
    MINIPLAYER_LMPRICE_CATALOG: path.join(home, 'catalog.json'),
    XDG_CACHE_HOME: path.join(home, 'cache'),
    PATH: bin + path.delimiter + process.env.PATH,
  });
  fs.writeFileSync(env.MINIPLAYER_CATALOG, JSON.stringify({schema_version: 1, records: []}));

  if (dialect === 'miniplayer') {
    const providers = ['deepseek', 'moonshot', 'ollama'];
    for (const provider of providers) {
      const apiKey = provider === 'ollama' ? 'ollama' : original.key;
      fs.writeFileSync(env.MINIPLAYER_CONFIG, JSON.stringify({
        provider: {
          [provider]: {
            apiKey,
            baseURL: mockUrl + '/v1'
          }
        }
      }));

      const modelId = `${provider}/fixture-miniplayer`;
      const cases = provider === 'ollama' ? ['success', 'bad-usage'] : ['success', 'bad-usage', 'auth-error'];

      for (const selected of cases) {
        fs.writeFileSync(config, JSON.stringify({ ...original, selected, provider }));
        const sessions = path.join(env.MINIPLAYER_HOME, 'sessions');
        const before = new Set(fs.existsSync(sessions) ? fs.readdirSync(sessions) : []);
        const script = path.join(home, 'case.lms');
        fs.writeFileSync(script, `r = lm_agent({provider:"miniplayer",model:${JSON.stringify(modelId)}}, "ping"); println(json_stringify(r));\n`);

        const r = spawnSync(process.env.LMSCRIPT_BIN || 'lmscript', ['--no-preflight', script], {env, cwd: home, encoding: 'utf8'});
        assert.equal(r.status, 0, `lmscript failed: ${r.stdout}\n${r.stderr}`);
        const result = JSON.parse(r.stdout.trim().split('\n').at(-1));

        const added = fs.readdirSync(sessions).filter(f => !before.has(f) && f.endsWith('.json'));
        assert.equal(added.length, 1, 'one fresh session');
        const record = JSON.parse(fs.readFileSync(path.join(sessions, added[0])));
        assert.equal(record.turns.length, 1);
        const turn = record.turns[0];
        assert.equal(turn.input.text, 'ping');

        if (selected === 'success') {
          assert.equal(result.status, 'ok');
          assert.equal(result.reply, 'pong');
          assert.equal(result.error, null);
          assert.equal(turn.status, 'complete');
          assert.equal(turn.output.text, 'pong');
          assert.deepEqual(turn.usage, {inputTokens: 10, outputTokens: 2, totalTokens: 12, reasoningTokens: 1, cachedInputTokens: 3});
        } else {
          assert.equal(result.status, 'error');
          assert.equal(result.reply, '');
          if (selected === 'bad-usage') {
            assert.match(result.error, /ACP RPC error: Invalid JSON response/);
          } else {
            assert.match(result.error, /ACP RPC error:/);
            // Verify no secret leak in the ACP error returned
            assert.ok(!result.error.includes(original.key), 'error must not leak secret');
          }
          assert.equal(turn.status, 'failed');
          assert.equal(turn.output, undefined);
          assert.equal(turn.usage, undefined, 'failed parse must not fabricate token facts');

          const logdir = path.join(home, '.lmctl');
          if (fs.existsSync(logdir)) {
            const logs = fs.readdirSync(logdir).filter(f => f.endsWith('.log')).flatMap(f => fs.readFileSync(path.join(logdir, f), 'utf8').trim().split('\n').filter(Boolean).map(l => JSON.parse(l)));
            const event = logs.findLast(e => e.pid === r.pid && e.provider === 'miniplayer' && e.event === 'transport_error');
            if (event) {
              assert.equal(event.error_class, 'Error');
              assert.equal(event.reason, 'provider_no_result');
            }
          }
        }
        console.log(`PASS published lmctl miniplayer ${provider} ${selected} status/session/usage/error_class`);
      }
    }
  } else if (dialect === 'anthropic') {
    fs.writeFileSync(env.MINIPLAYER_CONFIG, JSON.stringify({
      provider: {
        anthropic: {
          apiKey: original.key,
          baseURL: mockUrl + '/v1'
        }
      }
    }));
    const modelId = 'anthropic/fixture-claude';
    for (const selected of ['success', 'bad-usage', 'auth-invalid']) {
      fs.writeFileSync(config, JSON.stringify({ ...original, selected }));
      const sessions = path.join(env.MINIPLAYER_HOME, 'sessions');
      const before = new Set(fs.existsSync(sessions) ? fs.readdirSync(sessions) : []);
      const script = path.join(home, 'case.lms');
      fs.writeFileSync(script, `r = lm_agent({provider:"miniplayer",model:${JSON.stringify(modelId)}}, "ping"); println(json_stringify(r));\n`);

      const r = spawnSync(process.env.LMSCRIPT_BIN || 'lmscript', ['--no-preflight', script], {env, cwd: home, encoding: 'utf8'});
      assert.equal(r.status, 0, `lmscript failed: ${r.stdout}\n${r.stderr}`);
      const result = JSON.parse(r.stdout.trim().split('\n').at(-1));

      const added = fs.readdirSync(sessions).filter(f => !before.has(f) && f.endsWith('.json'));
      assert.equal(added.length, 1, 'one fresh session');
      const record = JSON.parse(fs.readFileSync(path.join(sessions, added[0])));
      assert.equal(record.turns.length, 1);
      const turn = record.turns[0];
      assert.equal(turn.input.text, 'ping');

      if (selected === 'success') {
        assert.equal(result.status, 'ok');
        assert.equal(result.reply, 'pong');
        assert.equal(result.error, null);
        assert.equal(turn.status, 'complete');
        assert.equal(turn.output.text, 'pong');
        assert.deepEqual(turn.usage, {inputTokens: 10, outputTokens: 2, totalTokens: 12});
      } else {
        assert.equal(result.status, 'error');
        assert.equal(result.reply, '');
        assert.match(result.error, /ACP RPC error:/);
        assert.ok(!result.error.includes(original.key), 'error must not leak secret');
        assert.equal(turn.status, 'failed');
        assert.equal(turn.output, undefined);
      }
      console.log(`PASS published lmctl anthropic ${selected} status/session/usage`);
    }
  } else if (dialect === 'gemini') {
    fs.writeFileSync(env.MINIPLAYER_CONFIG, JSON.stringify({
      provider: {
        gemini: {
          apiKey: original.key,
          baseURL: mockUrl + '/v1beta'
        }
      }
    }));
    const modelId = 'gemini/fixture-gemini';
    for (const selected of ['success', 'bad-usage', 'auth-invalid']) {
      fs.writeFileSync(config, JSON.stringify({ ...original, selected }));
      const sessions = path.join(env.MINIPLAYER_HOME, 'sessions');
      const before = new Set(fs.existsSync(sessions) ? fs.readdirSync(sessions) : []);
      const script = path.join(home, 'case.lms');
      fs.writeFileSync(script, `r = lm_agent({provider:"miniplayer",model:${JSON.stringify(modelId)}}, "ping"); println(json_stringify(r));\n`);

      const r = spawnSync(process.env.LMSCRIPT_BIN || 'lmscript', ['--no-preflight', script], {env, cwd: home, encoding: 'utf8'});
      assert.equal(r.status, 0, `lmscript failed: ${r.stdout}\n${r.stderr}`);
      const result = JSON.parse(r.stdout.trim().split('\n').at(-1));

      const added = fs.readdirSync(sessions).filter(f => !before.has(f) && f.endsWith('.json'));
      assert.equal(added.length, 1, 'one fresh session');
      const record = JSON.parse(fs.readFileSync(path.join(sessions, added[0])));
      assert.equal(record.turns.length, 1);
      const turn = record.turns[0];
      assert.equal(turn.input.text, 'ping');

      if (selected === 'success') {
        assert.equal(result.status, 'ok');
        assert.equal(result.reply, 'pong');
        assert.equal(result.error, null);
        assert.equal(turn.status, 'complete');
        assert.equal(turn.output.text, 'pong');
        assert.deepEqual(turn.usage, {inputTokens: 10, outputTokens: 2, totalTokens: 12});
      } else {
        assert.equal(result.status, 'error');
        assert.equal(result.reply, '');
        assert.match(result.error, /ACP RPC error:/);
        assert.ok(!result.error.includes(original.key), 'error must not leak secret');
        assert.equal(turn.status, 'failed');
        assert.equal(turn.output, undefined);
      }
      console.log(`PASS published lmctl gemini ${selected} status/session/usage`);
    }
  }
} finally {
  fs.rmSync(home, {recursive: true, force: true});
  fs.writeFileSync(config, JSON.stringify(original));
}
