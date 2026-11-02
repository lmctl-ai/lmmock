// Trusted fixture-only host. Never used by production lmmock serving.
import fs from 'node:fs';
import path from 'node:path';
const [output,file]=process.argv.slice(2);
const invocation=JSON.parse(fs.readFileSync(process.env.LMCODE_INVOCATION_FILE,'utf8'));
if(invocation.version!==1||invocation.mode!=='single'||invocation.parts.length!==1||invocation.parts[0].file!==file)throw Error('invalid capture invocation');
if(!invocation.dry){fs.mkdirSync(path.dirname(output),{recursive:true});fs.rmSync(output,{force:true});fs.copyFileSync(file,output);fs.chmodSync(output,0o600);}
console.log('LMCODE_RETURN_BEGIN');
console.log(JSON.stringify({ok:true,result:null,...(invocation.dry?{dry:true}:{})}));
console.log('LMCODE_RETURN_END');
