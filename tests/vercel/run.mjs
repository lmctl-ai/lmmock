import {mkdtempSync,writeFileSync,rmSync} from 'node:fs';
import {tmpdir} from 'node:os';
import {resolve,join} from 'node:path';
import {spawnSync} from 'node:child_process';
const dir=mkdtempSync(join(tmpdir(),'lmmock-vercel-'));
try {
 const config=join(dir,'config.json');
 writeFileSync(config,JSON.stringify({apiKey:'fixture-key'}));
 const r=spawnSync(resolve(process.argv[2] || 'build/lmmock'),[
  'exec','--lmcode','build/llm-api.dslb','--config',config,'--',
  process.execPath,'tests/vercel/acceptance.mjs'
 ],{stdio:'inherit',env:{...process.env,MOCK_CONFIG:config},timeout:120000});
 if(r.error) throw r.error;
 process.exitCode=r.status ?? 1;
} finally {rmSync(dir,{recursive:true,force:true});}
