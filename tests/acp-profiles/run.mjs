import {profiles,manifest,runCase} from './run-case.mjs';
for(const profile of Object.keys(profiles))for(const item of manifest(profile).cases){await runCase(item.id,{quiet:true});console.log('PASS '+item.id);}
