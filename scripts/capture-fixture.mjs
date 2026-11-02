#!/usr/bin/env node
// Test/build-only adapter: run_lmcode owns compilation and its private program file.
// Capture the host input for embedded presets and byte-level negative controls.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
import {resolveLmscriptCommand} from '../tests/provider-compat/lmscript-command.mjs';
const [input,output]=process.argv.slice(2);
if (!input || !output) throw Error('usage: capture-fixture.mjs SOURCE.lms OUTPUT.lmcode');
const original=fs.readFileSync(input,'utf8').replace(/schema=("[^"]+")/g, (_,q)=>'schema='+JSON.stringify(path.resolve(path.dirname(input),JSON.parse(q))));
const end=original.lastIndexOf('end _lmcode_');
const match=original.match(/(\w+)\s*=\s*_lmcode_/);
if(end<0||!match) throw Error('fixture must contain an lmcode program');
const dir=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock-fixture-'));
try {
 const source=path.join(dir,'fixture.lms');
 const command=[process.execPath,path.resolve(import.meta.dirname,'capture-host.mjs'),path.resolve(output),'$DSLFILE'];
 fs.writeFileSync(source, original.slice(0,end+'end _lmcode_'.length)+'\nr=run_lmcode('+match[1]+',{command:'+JSON.stringify(command)+'});\nrequire(r.ok,"fixture capture failed");\n');
 const cli=resolveLmscriptCommand();
 const r=spawnSync(cli.command,[...cli.args,'--base-dir',path.dirname(path.resolve(input)),source],{stdio:'inherit'});
 if(r.error)throw r.error;
 process.exitCode=r.status??1;
}finally{fs.rmSync(dir,{recursive:true,force:true});}
