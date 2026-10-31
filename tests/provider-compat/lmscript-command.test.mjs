import assert from 'node:assert/strict';
import {mkdtempSync, mkdirSync, writeFileSync, rmSync} from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {parseNpmCmdShim} from './lmscript-command.mjs';

const dir = mkdtempSync(path.join(os.tmpdir(), 'lmmock-npm-shim-'));
try {
  const shim = path.join(dir, 'lmscript.cmd');
  const entry = path.join(dir, '..', '@lmctl-ai', 'lmctl', 'bin', 'lmscript');
  const source = '@ECHO off\r\nSETLOCAL\r\nnode "%dp0%\\..\\@lmctl-ai\\lmctl\\bin\\lmscript" %*\r\n';
  const command = parseNpmCmdShim(shim, source);
  assert.equal(command.command, process.execPath);
  assert.equal(command.args[0], path.resolve(path.dirname(shim), '..', '@lmctl-ai', 'lmctl', 'bin', 'lmscript'));
  assert.throws(() => parseNpmCmdShim(shim, '@echo unsupported %*'), /unsupported npm lmscript shim/);
  console.log('PASS Windows npm shim resolves to its JavaScript entrypoint; malformed shim is refused');
} finally { rmSync(dir, {recursive: true, force: true}); }
