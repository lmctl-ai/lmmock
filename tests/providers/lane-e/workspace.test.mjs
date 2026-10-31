import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {workspace, projectKey} from './case.mjs';

const root=fs.mkdtempSync(path.join(os.tmpdir(),'lane-e-cwd-'));
try {
 // Linux's existing procfs alias reproduces macOS /var vs /private/var without
 // creating a symlink or involving a provider. Native CI checks its own cwd too.
 const alias=process.platform==='linux'?'/proc/self/root'+root:root;
 const work=workspace(alias);
 const child=spawnSync(process.execPath,['-e','process.stdout.write(process.cwd())'],{cwd:work,encoding:'utf8'});
 assert.equal(child.status,0,child.stderr);
 assert.equal(work,child.stdout,'fixture cwd must equal the native client cwd');
 assert.equal(projectKey(work),projectKey(child.stdout),'session directory identity must match');
 console.log('PASS native fixture cwd and session directory identity');
} finally { fs.rmSync(root,{recursive:true,force:true}); }
