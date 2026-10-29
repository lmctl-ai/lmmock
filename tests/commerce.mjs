// Header-driven paging termination and config overrides, beyond the JSON suites.
import assert from 'node:assert/strict';
const [service, mode] = process.argv.slice(2);
const origin = process.env.LMMOCK_URL;
const shop = service === 'shopify', wp = service === 'wordpress';
const base = shop ? '/admin/api/2024-07/products.json' : wp ? '/wp-json/wp/v2/posts' : '/wp-json/wc/v3/products';
const headers = custom => shop ? {'X-Shopify-Access-Token': custom ? 'override-token' : 'fixture-token'} : {
  Authorization: 'Basic ' + Buffer.from(custom ? 'override-user:override-secret' : wp ? 'fixture-user:fixture-password' : 'ck_fixture:cs_fixture').toString('base64'),
};
const get = (url, custom = false) => fetch(url, {headers:headers(custom), signal:AbortSignal.timeout(5000)});
if (mode === 'override') {
  assert.equal((await get(origin + base)).status, 401);
  assert.equal((await get(origin + base, true)).status, 200);
  if (!shop && !wp) {
    assert.equal((await fetch(origin + base + '?consumer_key=override-user&consumer_secret=override-secret')).status, 200);
    assert.equal((await fetch(origin + base + '?consumer_key=ck_fixture&consumer_secret=cs_fixture')).status, 401);
  }
} else {
  let url = origin + base, pages = 0;
  const ids = [];
  while (url) {
    assert.ok(++pages <= 2, 'pagination must terminate');
    assert.equal(new URL(url).origin, origin, 'paging stays inside the fixture');
    const r = await get(url); assert.equal(r.status, 200);
    const body = await r.json(); ids.push(...(shop ? body.products : body).map(x => x.id));
    if (wp) {
      assert.equal(r.headers.get('x-wp-total'), '2');
      const total = Number(r.headers.get('x-wp-totalpages')); assert.equal(total, 2);
      url = pages < total ? origin + base + '?page=' + (pages + 1) : null;
    } else {
      // WooCommerce's n8n transport expects Link even on the final page.
      const link = r.headers.get('link'); assert.ok(link);
      url = link.match(/<([^>]+)>; rel="next"/)?.[1] || null;
    }
  }
  assert.deepEqual(ids, [1, 2]);
}
console.log(`PASS ${service} ${mode}`);
