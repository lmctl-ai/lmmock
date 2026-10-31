import fs from 'node:fs/promises';import os from 'node:os';import path from 'node:path';
import {loadFixture,executeProcess,checkExpectation} from '../../provider-fixtures/run.mjs';
import {runProcessSuite} from '../../provider-fixtures/process-suite.mjs';
const tmp=await fs.mkdtemp(path.join(os.tmpdir(),'codex-manifest-'));
try{await runProcessSuite(await loadFixture('fixtures/providers/codex-app-server.json'),tmp,{executeProcess,checkExpectation});}finally{await fs.rm(tmp,{recursive:true,force:true});}
