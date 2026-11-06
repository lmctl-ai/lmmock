// usage: node tests/replies.mjs build/lmmock   (spawns `lmmock exec --builtin llm-api` and runs itself as the client)
// The `replies` config scripts a multi-turn conversation: the rule whose needle occurs latest in the raw request
// body wins, so a client that resends its history gets the reply for its NEWEST turn on every provider route.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';

const replies = [
  { contains: 'first number is 3', text: 'Noted: the first number is 3.' },
  { contains: 'second number is 7', text: 'Noted: the second number is 7.' },
  { contains: 'What is the total', text: '10' },
];
if (process.argv[3] !== 'client') {
  const binary = path.resolve(process.argv[2] || 'build/lmmock');
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'lmmock replies '));
  const config = path.join(dir, 'llm.json');
  fs.writeFileSync(config, JSON.stringify({ apiKey: 'fixture-key', text: 'unscripted', toolCalls: false, replies }));
  try {
    const r = spawnSync(binary, ['exec', '--builtin', 'llm-api', '--config', config, '--', process.execPath, import.meta.filename, binary, 'client'],
      { env: { ...process.env, MOCK_CONFIG: config }, encoding: 'utf8', timeout: 60000 });
    process.stdout.write(r.stdout); process.stderr.write(r.stderr);
    if (r.status !== 0) process.exitCode = 1;
  } finally { fs.rmSync(dir, { recursive: true, force: true }); }
} else {
  const origin = process.env.LMMOCK_URL;
  assert.ok(origin, 'LMMOCK_URL is set by lmmock exec');
  const turns = ['The first number is 3.', 'The second number is 7.', 'What is the total?'];
  const expected = replies.map(rule => rule.text);
  const json = async (url, body, headers = {}) => {
    const response = await fetch(origin + url, { method: 'POST', headers: { 'Content-Type': 'application/json', ...headers }, body: JSON.stringify(body) });
    const text = await response.text();
    assert.equal(response.status, 200, `${url}: ${text}`);
    return JSON.parse(text);
  };
  // OpenAI chat completions: the whole history is resent, the newest user turn is serialized last.
  {
    const history = [];
    for (const [i, turn] of turns.entries()) {
      history.push({ role: 'user', content: turn });
      const r = await json('/v1/chat/completions', { model: 'mock-agent', messages: history }, { Authorization: 'Bearer fixture-key' });
      const reply = r.choices[0].message.content;
      assert.equal(reply, expected[i], `chat turn ${i + 1}`);
      history.push({ role: 'assistant', content: reply });
    }
    console.log('PASS chat completions: scripted replies follow the newest turn (3, 7 -> 10)');
  }
  // OpenAI responses with a structured input list.
  {
    const input = [];
    for (const [i, turn] of turns.entries()) {
      input.push({ role: 'user', content: [{ type: 'input_text', text: turn }] });
      const r = await json('/v1/responses', { model: 'mock-agent', input }, { Authorization: 'Bearer fixture-key' });
      const reply = r.output[0].content[0].text;
      assert.equal(reply, expected[i], `responses turn ${i + 1}`);
      input.push({ role: 'assistant', content: [{ type: 'output_text', text: reply }] });
    }
    console.log('PASS responses: scripted replies follow the newest turn');
  }
  // Anthropic messages.
  {
    const messages = [];
    for (const [i, turn] of turns.entries()) {
      messages.push({ role: 'user', content: [{ type: 'text', text: turn }] });
      const r = await json('/v1/messages', { model: 'claude-mock', max_tokens: 64, messages }, { 'x-api-key': 'fixture-key', 'anthropic-version': '2023-06-01' });
      const reply = r.content[0].text;
      assert.equal(reply, expected[i], `messages turn ${i + 1}`);
      messages.push({ role: 'assistant', content: [{ type: 'text', text: reply }] });
    }
    console.log('PASS anthropic messages: scripted replies follow the newest turn');
  }
  // Gemini generateContent.
  {
    const contents = [];
    for (const [i, turn] of turns.entries()) {
      contents.push({ role: 'user', parts: [{ text: turn }] });
      const r = await json('/v1beta/models/gemini-mock:generateContent', { contents }, { 'x-goog-api-key': 'fixture-key' });
      const reply = r.candidates[0].content.parts[0].text;
      assert.equal(reply, expected[i], `gemini turn ${i + 1}`);
      contents.push({ role: 'model', parts: [{ text: reply }] });
    }
    console.log('PASS gemini generateContent: scripted replies follow the newest turn');
  }
  // Ollama chat.
  {
    const messages = [];
    for (const [i, turn] of turns.entries()) {
      messages.push({ role: 'user', content: turn });
      const r = await json('/api/chat', { model: 'mock-agent', messages, stream: false }, { Authorization: 'Bearer fixture-key' });
      const reply = r.message.content;
      assert.equal(reply, expected[i], `ollama turn ${i + 1}`);
      messages.push({ role: 'assistant', content: reply });
    }
    console.log('PASS ollama chat: scripted replies follow the newest turn');
  }
  // No rule matches: `text` still wins over the default pong; a tie keeps the first listed rule.
  {
    const r = await json('/v1/chat/completions', { model: 'mock-agent', messages: [{ role: 'user', content: 'ping' }] }, { Authorization: 'Bearer fixture-key' });
    assert.equal(r.choices[0].message.content, 'unscripted', 'unmatched request falls back to text');
    fs.writeFileSync(process.env.MOCK_CONFIG, JSON.stringify({ apiKey: 'fixture-key', toolCalls: false, replies: [{ contains: 'same', text: 'first' }, { contains: 'same', text: 'second' }] }));
    const tie = await json('/v1/chat/completions', { model: 'mock-agent', messages: [{ role: 'user', content: 'same same' }] }, { Authorization: 'Bearer fixture-key' });
    assert.equal(tie.choices[0].message.content, 'first', 'a tie keeps the first listed rule');
    fs.writeFileSync(process.env.MOCK_CONFIG, JSON.stringify({ apiKey: 'fixture-key', toolCalls: false, replies: [] }));
    const none = await json('/v1/chat/completions', { model: 'mock-agent', messages: [{ role: 'user', content: 'ping' }] }, { Authorization: 'Bearer fixture-key' });
    assert.equal(none.choices[0].message.content, 'pong', 'no rules and no text keep the default reply');
    console.log('PASS fallback: text, first-listed tie rule, then the default pong');
  }
  // delayMs holds the response: a client can observe an in-flight turn.
  {
    fs.writeFileSync(process.env.MOCK_CONFIG, JSON.stringify({ apiKey: 'fixture-key', toolCalls: false, replies: [{ contains: 'slow turn', text: 'late', delayMs: 700 }] }));
    const started = Date.now();
    const r = await json('/v1/chat/completions', { model: 'mock-agent', messages: [{ role: 'user', content: 'a slow turn please' }] }, { Authorization: 'Bearer fixture-key' });
    const elapsed = Date.now() - started;
    assert.equal(r.choices[0].message.content, 'late');
    assert.ok(elapsed >= 600, `delayMs must hold the reply (took ${elapsed} ms)`);
    console.log('PASS delayMs: the scripted reply is held for the configured time');
  }
  // Control: the expected reply of the last turn must be observable; a wrong expectation fails here.
  const wrong = process.env.REPLIES_EXPECT_TOTAL;
  if (wrong !== undefined) {
    fs.writeFileSync(process.env.MOCK_CONFIG, JSON.stringify({ apiKey: 'fixture-key', toolCalls: false, replies }));
    const r = await json('/v1/chat/completions', { model: 'mock-agent', messages: turns.map(content => ({ role: 'user', content })) }, { Authorization: 'Bearer fixture-key' });
    assert.equal(r.choices[0].message.content, wrong, 'scripted total reply mismatch');
  }
}
