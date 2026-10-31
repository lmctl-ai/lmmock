// Bridge the declared process case to this lane's existing owned fake-CLI driver.
import assert from 'node:assert/strict';
import fs from 'node:fs';
import path from 'node:path';
import {runCase,root} from './client.mjs';
process.env.CODEX_FIXTURE_BIN=path.join(root,'build','codex-fixture'+(process.platform==='win32'?'.exe':''));
if(!process.env.LMCTL_PACKAGE_ROOT){for(const dir of process.env.PATH.split(path.delimiter)){const pkg=path.resolve(dir,'../@lmctl-ai/lmctl');if(fs.existsSync(path.join(pkg,'package.json'))){process.env.LMCTL_PACKAGE_ROOT=pkg;break;}}}
const evidence=runCase(process.env.LMMOCK_CODEX_CASE,{model:process.env.LMMOCK_CODEX_MODEL,resume:process.env.LMMOCK_CODEX_MODE==='resume',batch:process.env.LMMOCK_CODEX_MODE==='batch'});
try{
 assert.equal(evidence.exit,0,evidence.stdout+evidence.stderr);assert.ok(evidence.result,'published client must return JSON');
 const row=evidence.rows[0];const output={provider:evidence.result.result,row_count:evidence.rows.length,error_class:evidence.result.failure?.class,usage_reason:row?.usage_reason,cost_reason:row?.cost_reason};
 if(row?.tok_input_uncached!=null&&row?.tok_cache_read!=null)output.usage={input_tokens:row.tok_input_uncached+row.tok_cache_read,cached_input_tokens:row.tok_cache_read,output_tokens:row.tok_output,reasoning_output_tokens:row.tok_reasoning};
 console.log(JSON.stringify(output));process.stderr.write(evidence.stderr);
}finally{evidence.cleanup();}
