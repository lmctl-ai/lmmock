import {mkdtempSync, copyFileSync, rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {resolve, join} from 'node:path';
import {spawnSync} from 'node:child_process';
import {runFixture} from '../provider-fixtures/run.mjs';
import {resolveLmscriptCommand} from './lmscript-command.mjs';

const binary = resolve(process.argv[2] || 'build/lmmock');
const lmscriptBin = process.env.LMSCRIPT_BIN || 'lmscript';
const lmscript = resolveLmscriptCommand(lmscriptBin);
const published = process.argv.includes('--published');

const dialects = [
  { name: 'anthropic', handler: 'fixtures/anthropic/handler.lms', dslb: 'build/anthropic.dslb', wire: 'fixtures/anthropic/wire.json', manifest: 'fixtures/providers/anthropic-messages.json' },
  { name: 'gemini', handler: 'fixtures/gemini/handler.lms', dslb: 'build/gemini.dslb', wire: 'fixtures/gemini/wire.json', manifest: 'fixtures/providers/gemini-content.json' },
  { name: 'ollama', handler: 'fixtures/ollama/handler.lms', dslb: 'build/ollama.dslb', wire: 'fixtures/ollama/wire.json', manifest: 'fixtures/providers/ollama-ndjson.json' },
  { name: 'miniplayer', handler: 'fixtures/miniplayer/handler.lms', dslb: 'build/miniplayer.dslb', wire: 'fixtures/miniplayer/wire.json', manifest: 'fixtures/providers/miniplayer-routed.json' },
];

// Compile all handlers
for (const d of dialects) {
  const c = spawnSync(lmscript.command, [...lmscript.args, '--no-preflight', d.handler, d.dslb], { stdio: 'inherit' });
  if (c.error) throw c.error;
  if (c.status !== 0) throw new Error(`Compile failed for ${d.name}: ${c.status}`);
}

const dir = mkdtempSync(join(tmpdir(), 'lmmock-provider-compat-'));

try {
  if (published) {
    if (!process.env.MINIPLAYER_TEST_BIN) {
      console.log('Skipping published client test: MINIPLAYER_TEST_BIN not set');
    } else {
      for (const dialectName of ['miniplayer', 'anthropic', 'gemini']) {
        const d = dialects.find(x => x.name === dialectName);
        const config = join(dir, `${d.name}-wire.json`);
        copyFileSync(d.wire, config);
        const r = spawnSync(binary, [
          'exec',
          '--lmcode', d.dslb,
          '--config', config,
          '--',
          process.execPath, 'tests/provider-compat/published.mjs'
        ], {
          stdio: 'inherit',
          env: { ...process.env, DIALECT: d.name, MOCK_CONFIG: config }
        });
        if (r.error) throw r.error;
        if (r.status !== 0) throw new Error(`Published client test failed for ${d.name}: ${r.status}`);
      }
    }
  } else {
    // 1. Run provider-fixtures manifests against SCHEMA.json contract
    console.log('--- Running provider fixture manifests ---');
    for (const d of dialects) {
      const res = await runFixture(d.manifest, binary);
      console.log(`PASS ${d.name} manifest (${res.passed} cases)`);
    }

    // 2. Run failing controls for all 4 dialects
    console.log('--- Running negative failing controls ---');
    for (const d of dialects) {
      const config = join(dir, `${d.name}-wire.json`);
      copyFileSync(d.wire, config);
      const r = spawnSync(binary, [
        'exec',
        '--lmcode', d.dslb,
        '--config', config,
        '--',
        process.execPath, 'tests/provider-compat/failing-controls.mjs'
      ], {
        stdio: 'inherit',
        env: { ...process.env, DIALECT: d.name, MOCK_CONFIG: config }
      });
      if (r.error) throw r.error;
      if (r.status !== 0) throw new Error(`Failing controls failed for ${d.name}: ${r.status}`);
    }

    for (const name of ['gemini', 'ollama']) {
      const d = dialects.find(d => d.name === name);
      const r = spawnSync(binary, ['exec', '--lmcode', d.dslb, '--config', d.wire, '--', process.execPath, 'tests/provider-compat/regressions.mjs'], {stdio: 'inherit', env: {...process.env, DIALECT: name}});
      if (r.error) throw r.error;
      if (r.status !== 0) throw new Error(`Native dialect regressions failed: ${name}`);
    }

    // 3. Run mutations driver
    console.log('--- Running handler mutations ---');
    const m = spawnSync(process.execPath, ['tests/provider-compat/mutations.mjs', binary], {
      stdio: 'inherit',
      env: process.env
    });
    if (m.error) throw m.error;
    if (m.status !== 0) throw new Error(`Mutations driver failed: ${m.status}`);
  }
} finally {
  rmSync(dir, { recursive: true, force: true });
}
