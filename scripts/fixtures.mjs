import fs from 'node:fs';
import path from 'node:path';
const schema=JSON.stringify(path.resolve('examples/schema.json').replaceAll('\\','/'));
const handler='@ok\nfn handler(x: Exchange) -> void implements Handler { x.send("ok"); }\n';
const cases={missing:handler+'route("GET","/","missing");',duplicate:handler+'route("GET","/","ok");route("GET","/","ok");',conflict:handler+'route("GET","/a/{x}","ok");route("GET","/{y}/b","ok");','no-route':handler,'runtime-error':'@ok\nfn handler(x: Exchange) -> void implements Handler { x.status(999); }\nroute("GET","/","ok");'};
for(const [name,body] of Object.entries(cases))fs.writeFileSync(`build/${name}.lms`,`p = _lmcode_ schema=${schema};\n${body}\nend _lmcode_\nsavedsl(p,ARGV[0]);\n`);
fs.writeFileSync('build/not-found.lms',`p = _lmcode_ schema=${schema};\n@missing\nfn missing(x: Exchange) -> void implements Handler { x.status(404); x.send("mocked missing"); }\nroute("GET","/missing","missing");\nend _lmcode_\nsavedsl(p,ARGV[0]);\n`);
// A verified entry with the wrong interface but the exact host schema.
// The schema hash must match, so mutate only the entry's interface name in bytecode.
const good=fs.readFileSync('build/api.dslb');const wrong=Buffer.from(good);const needle=Buffer.from('interface Handler(Exchange) -> void');const at=wrong.indexOf(needle);if(at<0)throw Error('entry interface missing');Buffer.from('interface NotMock(Exchange) -> void').copy(wrong,at);fs.writeFileSync('build/wrong-interface.dslb',wrong);

const oldSchema=JSON.stringify(path.resolve("internal/mock/schema-v1.json").split(path.sep).join("/"));
fs.writeFileSync("build/legacy.lms", "p = _lmcode_ schema="+oldSchema+";\n"+handler+'route("GET","/","ok");\nend _lmcode_\nsavedsl(p,ARGV[0]);\n');
