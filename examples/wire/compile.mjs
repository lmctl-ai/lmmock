#!/usr/bin/env node
// Trusted fixture compilation adapter for the repository's published compiler pin.
// Runtime serving uses only the resulting bytes and does not require Node.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import {spawnSync} from 'node:child_process';
const [schema,input,output]=process.argv.slice(2);
if(!schema||!input||!output||process.argv.length!==5)throw Error('usage: node compile.mjs SCHEMA.json HANDLER.lm PROGRAM.dslb');
let compiler=process.env.LMSCRIPT_BIN||'lmscript';
if(!compiler.includes('/')&&!compiler.includes('\\')){
 const extensions=process.platform==='win32'?['.cmd','.exe','']:[''];
 compiler=(process.env.PATH||'').split(path.delimiter).flatMap(dir=>extensions.map(ext=>path.join(dir,compiler+ext))).find(file=>fs.existsSync(file));
 if(!compiler)throw Error('set LMSCRIPT_BIN to the npm lmscript compiler');
}
if(compiler.toLowerCase().endsWith('.cmd'))compiler=path.resolve(path.dirname(compiler),'../@lmctl-ai/lmctl/bin/lmscript');
const dir=fs.mkdtempSync(path.join(os.tmpdir(),'lmmock-wire-compile-'));
try{
 const source=path.join(dir,'program.lms');
 fs.writeFileSync(source,'program = _lmcode_ schema='+JSON.stringify(path.resolve(schema).replaceAll('\\','/'))+';\n'+fs.readFileSync(input,'utf8')+'\nend _lmcode_\nsavedsl(program,ARGV[0]);\n');
 const result=spawnSync(process.execPath,[compiler,'--no-preflight',source,path.resolve(output)],{stdio:'inherit'});
 if(result.error)throw result.error;if(result.status!==0)process.exitCode=result.status??1;
}finally{fs.rmSync(dir,{recursive:true,force:true});}
