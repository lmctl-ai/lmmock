import {profiles} from './run-case.mjs';
import {runFixture} from '../provider-fixtures/run.mjs';
for(const profile of Object.keys(profiles))await runFixture('fixtures/providers/acp-'+profile+'.json','build/lmmock');
