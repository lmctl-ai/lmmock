import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {fileURLToPath} from 'node:url';
import {checkExpectation, executeProcess, listFixtures, loadFixture, runFixture} from './run.mjs';

const repo = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
const manifestPath = path.join(repo, 'fixtures/providers/openai-chat.json');
const binary = path.resolve(process.argv[2] ?? 'build/lmmock');
const {manifest} = await loadFixture(manifestPath, repo);
assert.equal(manifest.dialect.id, 'openai-chat-completions');
assert.deepEqual(manifest.cases.map(c => c.kind), ['usage', 'error', 'model', 'error', 'tool', 'stream']);

let passed = 0;
for (const fixturePath of await listFixtures(repo)) {
  const {manifest: candidate} = await loadFixture(fixturePath, repo);
  if (candidate.server?.kind !== 'http') continue;
  const baseline = await runFixture(fixturePath, binary, repo);
  assert.equal(baseline.failed, 0);
  assert.equal(baseline.passed, candidate.cases.length);
  passed += baseline.passed;
}
process.stdout.write(`PASS provider fixture baseline (${passed} cases, all HTTP manifests)\n`);

const probeVars = {repo_root: repo, base_url: 'http://127.0.0.1:1', lmmock_url: 'http://127.0.0.1:1', tmpdir: os.tmpdir(), server_url_env: 'FIXTURE_BASE'};
const processRequest = {kind: 'process', executable: process.execPath, argv: ['-e', 'process.stdout.write(JSON.stringify({error_class:"auth_required", url:process.env.FIXTURE_BASE}))']};
const processResult = await executeProcess(processRequest, repo, probeVars);
const processCase = {id: 'runner-process-class', request: processRequest, expect: {exit_code: 0, error_class: 'auth_required', json: {url: probeVars.base_url}}};
await checkExpectation(processCase, processResult, probeVars);
await assert.rejects(() => checkExpectation({...processCase, expect: {exit_code: 0, error_class: 'unknown'}}, processResult, probeVars), /error_class expected/);
process.stdout.write('PASS shell-free process probe captures loopback env/error_class and rejects a wrong class\n');

const mutated = structuredClone(manifest);
mutated.cases.find(c => c.id === 'chat-success-and-usage').expect.usage.prompt_tokens = 999;
const dir = await fs.mkdtemp(path.join(os.tmpdir(), 'lmmock-fixture-control-'));
try {
  const mutantPath = path.join(dir, 'mutated.json');
  await fs.writeFile(mutantPath, JSON.stringify(mutated));
  await assert.rejects(() => runFixture(mutantPath, binary, repo), /chat-success-and-usage: usage expected/);
  process.stdout.write('PASS mutation control: wrong usage expectation is caught\n');

  mutated.source.reference = '';
  await fs.writeFile(mutantPath, JSON.stringify(mutated));
  await assert.rejects(() => loadFixture(mutantPath, repo), /source.reference: string is too short/);
  process.stdout.write('PASS malformed-manifest control: missing evidence reference is refused\n');

  const handlerDir = path.join(dir, 'fixtures');
  await fs.mkdir(handlerDir);
  const handlerSource = await fs.readFile(path.join(repo, 'examples/llm.lms'), 'utf8');
  const brokenHandler = handlerSource.replace('let content = "pong"', 'let content = "mutated"');
  assert.notEqual(brokenHandler, handlerSource, 'handler mutation anchor exists');
  await fs.writeFile(path.join(handlerDir, 'handler.lms'), brokenHandler);
  await fs.copyFile(path.join(repo, 'examples/schema.json'), path.join(handlerDir, 'schema.json'));
  await fs.copyFile(path.join(repo, 'examples/llm-config.json'), path.join(handlerDir, 'config.json'));
  const handlerMutation = structuredClone(manifest);
  handlerMutation.server.handler = 'fixtures/handler.lms';
  handlerMutation.server.config = 'fixtures/config.json';
  await fs.writeFile(mutantPath, JSON.stringify(handlerMutation));
  await assert.rejects(() => runFixture(mutantPath, binary, dir), /chat-success-and-usage: json choices.0.message.content expected/);
  process.stdout.write('PASS handler mutation control: changed response text is caught\n');
} finally {
  await fs.rm(dir, {recursive: true, force: true});
}
