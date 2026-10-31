// End-to-end integration test for @github/copilot CLI against lmmock.
import assert from 'node:assert/strict';
import {spawnSync} from 'node:child_process';
import path from 'node:path';
import fs from 'node:fs';

const mockUrl = process.env.LMMOCK_URL;
assert.ok(mockUrl, 'LMMOCK_URL environment variable is required (run via lmmock exec)');

const configFile = process.env.MOCK_CONFIG;
const initialConfig = configFile && fs.existsSync(configFile) ? fs.readFileSync(configFile, 'utf8') : null;

// Check if copilot CLI is installed
const checkCopilot = spawnSync('copilot', ['--version'], {encoding: 'utf8'});
if (checkCopilot.error || checkCopilot.status !== 0) {
  console.log('SKIP: copilot CLI is not installed or not in PATH');
  process.exit(0);
}

try {
  console.log(`Testing @github/copilot CLI against lmmock at ${mockUrl}`);

function setConfig(opts) {
  if (configFile) {
    fs.writeFileSync(configFile, JSON.stringify({
      apiKey: 'copilot-test-key',
      text: 'Hello from Copilot mock! Integration test verified.',
      ...opts,
    }));
  }
}

function runCopilot(args, envOverrides = {}) {
  const env = {
    ...process.env,
    COPILOT_OFFLINE: 'true',
    COPILOT_ALLOW_ALL: 'true',
    ...envOverrides,
  };
  const res = spawnSync('copilot', args, {
    env,
    encoding: 'utf8',
    timeout: 30000,
  });
  if (res.error) throw res.error;
  return res;
}

// Test 1: OpenAI chat completions wire API
console.log('Testing Copilot with OpenAI Completions provider...');
setConfig({mode: 'normal'});
const r1 = runCopilot(['-p', 'Hello completions', '--allow-all-tools'], {
  COPILOT_PROVIDER_TYPE: 'openai',
  COPILOT_PROVIDER_BASE_URL: `${mockUrl}/v1`,
  COPILOT_PROVIDER_API_KEY: 'copilot-test-key',
  COPILOT_PROVIDER_WIRE_API: 'completions',
  COPILOT_MODEL: 'gpt-4o',
});
assert.equal(r1.status, 0, `copilot failed with status ${r1.status}: ${r1.stderr}\n${r1.stdout}`);
assert.match(r1.stdout, /Hello from Copilot mock/, 'response should contain mock text');
console.log('PASS: Copilot OpenAI Completions provider');

// Test 2: OpenAI responses wire API
console.log('Testing Copilot with OpenAI Responses provider...');
const r2 = runCopilot(['-p', 'Hello responses', '--allow-all-tools'], {
  COPILOT_PROVIDER_TYPE: 'openai',
  COPILOT_PROVIDER_BASE_URL: `${mockUrl}/v1`,
  COPILOT_PROVIDER_API_KEY: 'copilot-test-key',
  COPILOT_PROVIDER_WIRE_API: 'responses',
  COPILOT_MODEL: 'gpt-4o',
});
assert.equal(r2.status, 0, `copilot failed with status ${r2.status}: ${r2.stderr}\n${r2.stdout}`);
assert.match(r2.stdout, /Hello from Copilot mock/, 'response should contain mock text');
console.log('PASS: Copilot OpenAI Responses provider');

// Test 3: Anthropic messages API
console.log('Testing Copilot with Anthropic Messages provider...');
const r3 = runCopilot(['-p', 'Hello messages', '--allow-all-tools'], {
  COPILOT_PROVIDER_TYPE: 'anthropic',
  COPILOT_PROVIDER_BASE_URL: mockUrl,
  COPILOT_PROVIDER_API_KEY: 'copilot-test-key',
  COPILOT_MODEL: 'claude-sonnet-4',
});
assert.equal(r3.status, 0, `copilot failed with status ${r3.status}: ${r3.stderr}\n${r3.stdout}`);
assert.match(r3.stdout, /Hello from Copilot mock/, 'response should contain mock text');
console.log('PASS: Copilot Anthropic Messages provider');

// Test 4: Tool execution round-trip
if (configFile) {
  console.log('Testing Copilot tool execution round-trip...');
  setConfig({mode: 'tool'});
  const r4 = runCopilot(['-p', 'Run verification', '--allow-all-tools'], {
    COPILOT_PROVIDER_TYPE: 'openai',
    COPILOT_PROVIDER_BASE_URL: `${mockUrl}/v1`,
    COPILOT_PROVIDER_API_KEY: 'copilot-test-key',
    COPILOT_PROVIDER_WIRE_API: 'responses',
    COPILOT_MODEL: 'gpt-4o',
  });
  assert.equal(r4.status, 0, `copilot tool test failed: ${r4.stderr}\n${r4.stdout}`);
  assert.match(r4.stdout, /tool-executed/, 'output should show tool execution');
  console.log('PASS: Copilot tool execution round-trip');
}

  console.log('ALL COPILOT INTEGRATION TESTS PASSED!');
} finally {
  if (configFile && initialConfig !== null) {
    fs.writeFileSync(configFile, initialConfig);
  }
}
