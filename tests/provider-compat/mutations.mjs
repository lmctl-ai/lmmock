// Mutation test driver for handler families:
// anthropic, gemini, ollama, miniplayer.
// For each handler family:
// - baseline compilation passes
// - mutated handler (bypassing auth or breaking contract) compiles and is caught by the test
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import os from 'node:os';
import {spawnSync} from 'node:child_process';

const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'lmmock-mutations-'));
const binary = path.resolve(process.argv[2] || 'build/lmmock');
const lmscriptBin = process.env.LMSCRIPT_BIN || 'lmscript';
const schemaPath = JSON.stringify(path.resolve('examples/schema.json').replaceAll('\\', '/'));

const handlers = [
  {
    family: 'anthropic',
    sourceFile: 'fixtures/anthropic/handler.lms',
    configFile: 'fixtures/anthropic/wire.json',
    target: 'secure_equal(key, cfg.get("key").as_string())',
    mutant: 'true',
  },
  {
    family: 'gemini',
    sourceFile: 'fixtures/gemini/handler.lms',
    configFile: 'fixtures/gemini/wire.json',
    target: 'secure_equal(key, cfg.get("key").as_string())',
    mutant: 'true',
  },
  {
    family: 'ollama',
    sourceFile: 'fixtures/ollama/handler.lms',
    configFile: 'fixtures/ollama/wire.json',
    target: 'secure_equal(auth, "Bearer " + cfg.get("key").as_string())',
    mutant: 'true',
  },
  {
    family: 'miniplayer',
    sourceFile: 'fixtures/miniplayer/handler.lms',
    configFile: 'fixtures/miniplayer/wire.json',
    target: 'secure_equal(auth, "Bearer " + cfg.get("key").as_string())',
    mutant: 'true',
  },
];

try {
  for (const h of handlers) {
    const rawSource = fs.readFileSync(h.sourceFile, 'utf8');
    const sourceWithSchema = rawSource.replace('"../../examples/schema.json"', schemaPath);
    const tempConfig = path.join(dir, `${h.family}-config.json`);
    fs.copyFileSync(h.configFile, tempConfig);

    // 1. Baseline: must pass
    const baseScript = path.join(dir, `${h.family}-base.lms`);
    const baseBin = path.join(dir, `${h.family}-base.bin`);
    fs.writeFileSync(baseScript, sourceWithSchema);
    const compileBase = spawnSync('bash', ['-c', '"$LMSCRIPT_BIN" --no-preflight "$1" "$2"', 'compile', baseScript, baseBin], {
      encoding: 'utf8',
      env: { ...process.env, LMSCRIPT_BIN: lmscriptBin }
    });
    assert.equal(compileBase.status, 0, `Baseline compile failed for ${h.family}: ${compileBase.stderr}`);

    const runBase = spawnSync(binary, ['exec', '--lmcode', baseBin, '--config', tempConfig, '--', process.execPath, 'tests/provider-compat/failing-controls.mjs'], {
      encoding: 'utf8',
      env: { ...process.env, DIALECT: h.family, MOCK_CONFIG: tempConfig }
    });
    assert.equal(runBase.status, 0, `Baseline run failed for ${h.family}: ${runBase.stdout}\n${runBase.stderr}`);
    console.log(`PASS ${h.family} baseline handler`);

    // 2. Mutant: must compile cleanly and be caught by the test
    assert.ok(sourceWithSchema.includes(h.target), `Target string not found in ${h.sourceFile}`);
    const mutatedSource = sourceWithSchema.replace(h.target, h.mutant);
    const mutantScript = path.join(dir, `${h.family}-mutant.lms`);
    const mutantBin = path.join(dir, `${h.family}-mutant.bin`);
    fs.writeFileSync(mutantScript, mutatedSource);
    const compileMutant = spawnSync('bash', ['-c', '"$LMSCRIPT_BIN" --no-preflight "$1" "$2"', 'compile', mutantScript, mutantBin], {
      encoding: 'utf8',
      env: { ...process.env, LMSCRIPT_BIN: lmscriptBin }
    });
    assert.equal(compileMutant.status, 0, `Mutant compile failed for ${h.family}: ${compileMutant.stderr}`);

    // Re-copy fresh config for the mutant run
    fs.copyFileSync(h.configFile, tempConfig);
    const runMutant = spawnSync(binary, ['exec', '--lmcode', mutantBin, '--config', tempConfig, '--', process.execPath, 'tests/provider-compat/failing-controls.mjs'], {
      encoding: 'utf8',
      env: { ...process.env, DIALECT: h.family, MOCK_CONFIG: tempConfig }
    });
    assert.notEqual(runMutant.status, 0, `Mutant for ${h.family} was NOT caught! Run status was 0.`);
    console.log(`PASS ${h.family} handler mutation was caught: ${runMutant.stderr.split('\n')[0]}`);
  }
} finally {
  fs.rmSync(dir, { recursive: true, force: true });
}
