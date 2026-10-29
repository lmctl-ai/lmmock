import assert from 'node:assert/strict';
assert.ok(process.env.LMMOCK_URL, 'run this client through lmmock exec');
const response = await fetch(process.env.LMMOCK_URL + '/health');
assert.equal(response.status, 200);
assert.deepEqual(await response.json(), {ok:true});
console.log('PASS mock API client');
