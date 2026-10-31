import fs from 'node:fs/promises';
import os from 'node:os';
import path from 'node:path';
import {profiles} from './run-case.mjs';
import {loadFixture,executeProcess,checkExpectation} from '../provider-fixtures/run.mjs';
import {runProcessSuite} from '../provider-fixtures/process-suite.mjs';
const tmp=await fs.mkdtemp(path.join(os.tmpdir(),'acp-manifests-'));
try{for(const profile of Object.keys(profiles))await runProcessSuite(await loadFixture('fixtures/providers/acp-'+profile+'.json'),tmp,{executeProcess,checkExpectation});}
finally{await fs.rm(tmp,{recursive:true,force:true});}
