import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {spawn} from 'node:child_process';
import {fileURLToPath, pathToFileURL} from 'node:url';
import readline from 'node:readline';

const here = path.dirname(fileURLToPath(import.meta.url));
const defaultRoot = path.resolve(here, '../..');
const schemaFile = path.join(defaultRoot, 'fixtures/providers/SCHEMA.json');
const faultFile = path.join(defaultRoot, 'fixtures/providers/FAULTS.json');

function pointer(root, ref) {
  if (!ref.startsWith('#/')) throw new Error(`unsupported schema ref ${ref}`);
  return ref.slice(2).split('/').map(x => x.replaceAll('~1', '/').replaceAll('~0', '~')).reduce((v, k) => v?.[k], root);
}

function schemaErrors(value, rule, rootSchema, at = '$') {
  const errors = [];
  if (rule.$ref) return schemaErrors(value, pointer(rootSchema, rule.$ref), rootSchema, at);
  if (rule.oneOf) {
    const matches = rule.oneOf.filter(branch => schemaErrors(value, branch, rootSchema, at).length === 0).length;
    if (matches !== 1) errors.push(`${at}: must match exactly one supported shape`);
    return errors;
  }
  const typeOk = type => type === 'null' ? value === null
    : type === 'array' ? Array.isArray(value)
    : type === 'object' ? value !== null && typeof value === 'object' && !Array.isArray(value)
    : type === 'integer' ? Number.isSafeInteger(value)
    : type === 'number' ? typeof value === 'number' && Number.isFinite(value)
    : typeof value === type;
  if (rule.type) {
    const types = Array.isArray(rule.type) ? rule.type : [rule.type];
    if (!types.some(typeOk)) return [`${at}: expected ${types.join('|')}`];
  }
  if (Object.hasOwn(rule, 'const') && JSON.stringify(value) !== JSON.stringify(rule.const)) errors.push(`${at}: expected constant ${JSON.stringify(rule.const)}`);
  if (rule.enum && !rule.enum.some(item => JSON.stringify(item) === JSON.stringify(value))) errors.push(`${at}: unsupported value ${JSON.stringify(value)}`);
  if (typeof value === 'string') {
    if (rule.minLength !== undefined && value.length < rule.minLength) errors.push(`${at}: string is too short`);
    if (rule.pattern && !(new RegExp(rule.pattern).test(value))) errors.push(`${at}: string does not match ${rule.pattern}`);
  }
  if (typeof value === 'number') {
    if (rule.minimum !== undefined && value < rule.minimum) errors.push(`${at}: number is below minimum ${rule.minimum}`);
    if (rule.maximum !== undefined && value > rule.maximum) errors.push(`${at}: number is above maximum ${rule.maximum}`);
  }
  if (Array.isArray(value)) {
    if (rule.minItems !== undefined && value.length < rule.minItems) errors.push(`${at}: array is too short`);
    if (rule.uniqueItems && new Set(value.map(item => JSON.stringify(item))).size !== value.length) errors.push(`${at}: duplicate array item`);
    if (rule.items) value.forEach((item, index) => errors.push(...schemaErrors(item, rule.items, rootSchema, `${at}[${index}]`)));
  }
  if (value !== null && typeof value === 'object' && !Array.isArray(value)) {
    for (const key of rule.required ?? []) if (!Object.hasOwn(value, key)) errors.push(`${at}.${key}: required`);
    for (const [key, child] of Object.entries(value)) {
      if (rule.properties && Object.hasOwn(rule.properties, key)) errors.push(...schemaErrors(child, rule.properties[key], rootSchema, `${at}.${key}`));
      else if (rule.additionalProperties === false) errors.push(`${at}.${key}: unknown property`);
      else if (rule.additionalProperties && typeof rule.additionalProperties === 'object') errors.push(...schemaErrors(child, rule.additionalProperties, rootSchema, `${at}.${key}`));
    }
  }
  return errors;
}

export async function loadFixture(manifestPath, root = defaultRoot) {
  const absolute = path.resolve(root, manifestPath);
  const manifest = JSON.parse(await fs.readFile(absolute, 'utf8'));
  const schema = JSON.parse(await fs.readFile(schemaFile, 'utf8'));
  const faults = JSON.parse(await fs.readFile(faultFile, 'utf8')).faults;
  const errors = schemaErrors(manifest, schema, schema);
  const cases = new Map((manifest.cases ?? []).map(c => [c.id, c]));
  const caseIds = manifest.cases?.map(c => c.id) ?? [];
  const modelIds = manifest.models?.map(m => m.id) ?? [];
  const routeIds = manifest.routes?.map(r => r.id) ?? [];
  const faultIds = manifest.faults?.map(f => f.id) ?? [];
  if (new Set(caseIds).size !== caseIds.length) errors.push('$.cases: duplicate case id');
  if (new Set(modelIds).size !== modelIds.length) errors.push('$.models: duplicate model id');
  if (new Set(routeIds).size !== routeIds.length) errors.push('$.routes: duplicate route id');
  if (new Set(faultIds).size !== faultIds.length) errors.push('$.faults: duplicate fault id');
  const knownFaults = new Set(faults.map(f => f.id));
  for (const [i, testCase] of (manifest.cases ?? []).entries()) {
    if (testCase.request?.kind === 'http' && !manifest.routes?.some(r => r.method === testCase.request.method && r.path === testCase.request.path)) errors.push(`$.cases[${i}].request: route is not declared`);
    for (const id of testCase.faults ?? []) if (!knownFaults.has(id)) errors.push(`$.cases[${i}].faults: unknown deterministic fault ${id}`);
  }
  for (const [i, fault] of (manifest.faults ?? []).entries()) {
    if (!cases.has(fault.trigger?.case_id)) errors.push(`$.faults[${i}].trigger.case_id: unknown case ${fault.trigger?.case_id}`);
    if (!knownFaults.has(fault.id)) errors.push(`$.faults[${i}].id: not in shared fault catalogue`);
  }
  for (const rel of [manifest.server?.handler, manifest.server?.config]) {
    if (!rel) continue;
    const full = path.resolve(root, rel);
    if (full !== root && !full.startsWith(root + path.sep)) errors.push(`$.server: path escapes fixture root: ${rel}`);
    else try { await fs.access(full); } catch { errors.push(`$.server: missing declared file ${rel}`); }
  }
  if (errors.length) throw new Error(`Invalid provider fixture ${manifest.id ?? '(unnamed)'}:\n${errors.join('\n')}`);
  return {manifest, root, absolute};
}

function getPath(value, dotted) {
  return dotted.split('.').reduce((current, part) => current == null ? undefined : current[part], value);
}

function eventsOf(text) {
  if (text.includes('\n\n') || text.includes('\r\n\r\n')) {
    return text.split(/\r?\n\r?\n/).filter(Boolean);
  }
  return text.split(/\r?\n/).filter(Boolean);
}

function expand(value, vars) {
  return value.replace(/\{\{([a-z_]+)\}\}/g, (_match, name) => {
    if (!Object.hasOwn(vars, name)) throw new Error(`unknown fixture variable {{${name}}}`);
    return vars[name];
  });
}

function startServer(binary, loaded, tempDir) {
  const {manifest, root} = loaded;
  const readyFile = path.join(tempDir, 'ready.json');
  const child = spawn(binary, ['serve', '--script', manifest.server.handler, '--config', manifest.server.config, '--ready-file', readyFile], {
    cwd: root,
    env: process.env,
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  let stderr = '';
  child.stderr.setEncoding('utf8');
  child.stderr.on('data', chunk => { stderr += chunk; });
  const exited = new Promise(resolve => child.once('close', (code, signal) => resolve({code, signal})));
  const ready = new Promise((resolve, reject) => {
    const lines = readline.createInterface({input: child.stdout});
    lines.once('line', line => {
      try { resolve(JSON.parse(line)); }
      catch { reject(new Error(`lmmock readiness was not JSON: ${line}`)); }
    });
    child.once('error', reject);
    child.once('close', code => reject(new Error(`lmmock exited before readiness (${code}): ${stderr}`)));
  });
  return {child, ready, exited, stop: async () => {
    if (child.exitCode === null && child.signalCode === null) child.kill('SIGTERM');
    if (child.exitCode === null && child.signalCode === null) await exited;
  }};
}

export async function executeProcess(request, root, vars) {
  const executable = expand(request.executable, vars);
  const argv = request.argv.map(arg => expand(arg, vars));
  const env = {...process.env};
  if (vars.server_url_env) env[vars.server_url_env] = vars.base_url;
  for (const [key, value] of Object.entries(request.env ?? {})) env[key] = expand(value, vars);
  const cwd = request.cwd ? path.resolve(root, expand(request.cwd, vars)) : root;
  return await new Promise((resolve, reject) => {
    const child = spawn(executable, argv, {cwd, env, stdio: ['pipe', 'pipe', 'pipe']});
    let stdout = '', stderr = '';
    child.stdout.setEncoding('utf8'); child.stderr.setEncoding('utf8');
    child.stdout.on('data', chunk => { stdout += chunk; });
    child.stderr.on('data', chunk => { stderr += chunk; });
    child.once('error', reject);
    child.once('close', (code, signal) => resolve({exitCode: code ?? 1, signal, stdout, stderr}));
    if (request.stdin !== undefined) child.stdin.end(expand(request.stdin, vars));
    else child.stdin.end();
  });
}

async function executeHttp(request, baseUrl) {
  const headers = new Headers(request.headers ?? {});
  const init = {method: request.method, headers};
  if (request.raw_body !== undefined) init.body = request.raw_body;
  else if (request.body !== undefined) {
    init.body = JSON.stringify(request.body);
    if (!headers.has('content-type')) headers.set('content-type', 'application/json');
  }
  const response = await fetch(new URL(request.path, baseUrl), init);
  const text = await response.text();
  let json;
  try { json = JSON.parse(text); } catch { /* raw or event stream response */ }
  return {status: response.status, headers: response.headers, text, json};
}

export async function checkExpectation(testCase, result, vars) {
  const expected = testCase.expect;
  const errors = [];
  const isHttp = testCase.request.kind === 'http';
  if (isHttp && expected.status !== undefined && result.status !== expected.status) errors.push(`status expected ${expected.status}, got ${result.status}`);
  if (!isHttp && expected.exit_code !== undefined && result.exitCode !== expected.exit_code) errors.push(`exit expected ${expected.exit_code}, got ${result.exitCode}`);
  const text = isHttp ? result.text : `${result.stdout}\n${result.stderr}`;
  if (expected.body_contains && !text.includes(expected.body_contains)) errors.push(`body missing ${JSON.stringify(expected.body_contains)}`);
  if (expected.stdout_contains && !result.stdout.includes(expected.stdout_contains)) errors.push(`stdout missing ${JSON.stringify(expected.stdout_contains)}`);
  if (expected.stderr_contains && !result.stderr.includes(expected.stderr_contains)) errors.push(`stderr missing ${JSON.stringify(expected.stderr_contains)}`);
  for (const [name, want] of Object.entries(expected.headers ?? {})) {
    const actual = isHttp ? result.headers.get(name) : undefined;
    if (actual !== want) errors.push(`header ${name} expected ${JSON.stringify(want)}, got ${JSON.stringify(actual)}`);
  }
  if (expected.json) {
    const source = isHttp ? result.json : (() => { try { return JSON.parse(result.stdout.trim()); } catch { return undefined; } })();
    if (source === undefined) errors.push('expected JSON response but output was not JSON');
    else for (const [key, want] of Object.entries(expected.json)) {
      const actual = getPath(source, key);
      if (JSON.stringify(actual) !== JSON.stringify(want)) errors.push(`json ${key} expected ${JSON.stringify(want)}, got ${JSON.stringify(actual)}`);
    }
  }
  if (expected.model !== undefined) {
    const source = isHttp ? result.json : (() => { try { return JSON.parse(result.stdout.trim()); } catch { return undefined; } })();
    const actual = source?.model ?? source?.selected_model?.id;
    if (actual !== expected.model) errors.push(`model expected ${JSON.stringify(expected.model)}, got ${JSON.stringify(actual)}`);
  }
  if (expected.usage !== undefined) {
    const source = isHttp ? result.json : (() => { try { return JSON.parse(result.stdout.trim()); } catch { return undefined; } })();
    const actual = source?.usage ?? source?.result?.usage;
    if (JSON.stringify(actual) !== JSON.stringify(expected.usage)) errors.push(`usage expected ${JSON.stringify(expected.usage)}, got ${JSON.stringify(actual)}`);
  }
  if (expected.tool_calls !== undefined) {
    const source = isHttp ? result.json : (() => { try { return JSON.parse(result.stdout.trim()); } catch { return undefined; } })();
    const actual = source?.tool_calls ?? source?.choices?.[0]?.message?.tool_calls ?? source?.result?.tool_calls;
    if (JSON.stringify(actual) !== JSON.stringify(expected.tool_calls)) errors.push(`tool_calls expected ${JSON.stringify(expected.tool_calls)}, got ${JSON.stringify(actual)}`);
  }
  if (expected.error_class !== undefined) {
    const source = isHttp ? result.json : (() => { try { return JSON.parse(result.stdout.trim()); } catch { return undefined; } })();
    const actual = source?.error_class ?? source?.error?.class ?? source?.result?.error_class;
    if (actual !== expected.error_class) errors.push(`error_class expected ${expected.error_class}, got ${JSON.stringify(actual)}`);
  }
  if (expected.events !== undefined) {
    const events = eventsOf(text);
    let next = 0;
    for (const event of events) if (next < expected.events.length && event.includes(expected.events[next])) next++;
    if (next !== expected.events.length) errors.push(`event sequence incomplete: matched ${next}/${expected.events.length}`);
  }
  for (const effect of expected.effects ?? []) {
    const full = path.resolve(expand(effect.path, vars));
    if (full !== vars.tmpdir && !full.startsWith(vars.tmpdir + path.sep)) {
      errors.push(`effect path escapes private temp directory: ${effect.path}`);
      continue;
    }
    let found = true;
    let content = '';
    try { content = await fs.readFile(full, 'utf8'); } catch { found = false; }
    if (found !== effect.exists) errors.push(`effect ${effect.path} exists=${found}, expected ${effect.exists}`);
    if (effect.contains !== undefined && !content.includes(effect.contains)) errors.push(`effect ${effect.path} does not contain ${JSON.stringify(effect.contains)}`);
  }
  if (errors.length) throw new Error(`${testCase.id}: ${errors.join('; ')}`);
}

export async function validateFixture(manifestPath, root = defaultRoot) {
  return loadFixture(manifestPath, root);
}

export async function listFixtures(root = defaultRoot) {
  const directory = path.join(root, 'fixtures/providers');
  const names = (await fs.readdir(directory)).filter(name => name.endsWith('.json') && name !== 'SCHEMA.json' && name !== 'FAULTS.json').sort();
  return names.map(name => path.join(directory, name));
}

export async function runFixture(manifestPath, binary, root = defaultRoot) {
  const loaded = await loadFixture(manifestPath, root);
  if (loaded.manifest.server.kind !== 'http') throw new Error(`${loaded.manifest.id}: ${loaded.manifest.server.kind} lifecycle must use its protocol-owned process runner`);
  const tempDir = await fs.mkdtemp(path.join(os.tmpdir(), 'lmmock-provider-fixture-'));
  if(loaded.manifest.server.kind==='stdio'){
    try { const {runProcessSuite}=await import('./process-suite.mjs');return await runProcessSuite(loaded,tempDir); }
    finally {await fs.rm(tempDir,{recursive:true,force:true});}
  }
  const server = startServer(path.resolve(root, binary), loaded, tempDir);
  try {
    let ready;
    try { ready = await server.ready; }
    catch (error) { throw new Error(`${loaded.manifest.id}: ${error instanceof Error ? error.message : String(error)}`); }
    if (typeof ready.url !== 'string') throw new Error('lmmock readiness did not include url');
    const vars = {base_url: ready.url, lmmock_url: ready.url, repo_root: root, tmpdir: tempDir, server_url_env: loaded.manifest.server.url_env ?? ''};
    const failed = [];
    for (const testCase of loaded.manifest.cases) {
      try {
        const result = testCase.request.kind === 'http'
          ? await executeHttp(testCase.request, ready.url)
          : await executeProcess(testCase.request, root, vars);
        await checkExpectation(testCase, result, vars);
        process.stdout.write(`PASS ${loaded.manifest.id}/${testCase.id}\n`);
      } catch (error) { failed.push(error instanceof Error ? error.message : String(error)); }
    }
    if (failed.length) throw new Error(failed.join('\n'));
    return {passed: loaded.manifest.cases.length, failed: 0};
  } finally {
    await server.stop();
    await fs.rm(tempDir, {recursive: true, force: true});
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(path.resolve(process.argv[1])).href) {
  const manifestPath = process.argv[2];
  const binary = process.argv[3];
  if (!manifestPath || !binary || process.argv.length !== 4) {
    process.stderr.write('usage: node tests/provider-fixtures/run.mjs MANIFEST LMMOCK_BINARY\n');
    process.exitCode = 2;
  } else {
    try {
      const result = await runFixture(manifestPath, binary);
      process.stdout.write(`PASS ${result.passed} provider fixture cases\n`);
    } catch (error) {
      process.stderr.write(`FAIL ${error instanceof Error ? error.message : String(error)}\n`);
      process.exitCode = 1;
    }
  }
}
