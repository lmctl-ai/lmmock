// A fixture client must never accidentally contact a provider or price service.
import http from 'node:http';import https from 'node:https';import {syncBuiltinESMExports} from 'node:module';
function allowed(value){
 let host;
 if(typeof value==='string'||value instanceof URL)host=new URL(value).hostname;
 else host=value?.hostname??value?.host??'localhost';
 host=String(host).replace(/^\[|\]$/g,'');
 if(host!=='::1')host=host.replace(/:\d+$/,'');
 if(!['127.0.0.1','localhost','::1'].includes(host))throw Error('fixture blocked non-loopback network access');
}
const fetch=globalThis.fetch;globalThis.fetch=(input,...rest)=>{try{allowed(input instanceof Request?input.url:input);}catch(error){return Promise.reject(error);}return fetch(input,...rest);};
for(const module of [http,https])for(const key of ['request','get']){const original=module[key];module[key]=function(...args){allowed(args[0]);return original.apply(this,args);};}
syncBuiltinESMExports();
