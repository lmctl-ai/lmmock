# Runtime JSON data from lmscript to lmcode

See [json-response.lms](../examples/json-response.lms) for the small example and
[llm-api.lms](../examples/llm-api.lms) for the full OpenAI, Anthropic and Gemini mock.

## JSON template renderer: Go reference implementation

The reusable, standard-library-only package is
[pkg/jsontemplate](../pkg/jsontemplate/render.go):

~~~go
result, err := jsontemplate.Render(templateJSON, contextJSON)
~~~

Its lmcode host adapter exposes:

~~~text
json_render(template: Json, context: Json) -> Json
~~~

A template is valid JSON with placeholders inside string values. The context
must be a JSON object and can contain arbitrarily nested JSON values:

~~~json
{
  "message": "Hello, <user.name>!",
  "profile": "<user.profile>",
  "firstId": "<items.0.id>"
}
~~~

Given this context:

~~~json
{
  "user": {"name": "World", "profile": {"enabled": true}},
  "items": [{"id": 7}]
}
~~~

The result is:

~~~json
{
  "message": "Hello, World!",
  "profile": {"enabled": true},
  "firstId": 7
}
~~~

### Portable contract

- A whole-value placeholder preserves the selected JSON type: string, number,
  boolean, null, array or object. It can also replace the template root.
- Inline placeholders accept strings, numbers, booleans and null. Numbers keep
  their JSON spelling; booleans and null become their JSON text. Objects and
  arrays in inline text are errors, avoiding ambiguous serialization.
- Paths use ASCII identifiers and dots; subsequent components can be decimal
  array indexes. Arrays use canonical zero-based indexes (0, 1, 2; no 01).
  Keys containing dots, hyphens or other punctuation are not addressable in
  this first API; entire objects containing such keys can still be inserted.
- Missing paths, invalid JSON and a non-object context produce errors. Error
  wording and object key order are not part of the portable contract.
- Only string values are interpreted, never object keys. Rendering is one pass:
  inserted values containing placeholders stay literal.
- Use << to emit a literal <, so "<<user.name>" produces "<user.name>".
  Other text that is not a recognized placeholder is preserved.
- Inputs remain unchanged. Large numbers retain their original JSON lexemes;
  the Go implementation does not convert them to float64.
- Angle-bracket placeholders do not overlap with lmscript ${...} interpolation.
  This is a small JSON-aware template language inspired by StringTemplate,
  not an implementation of its loops, conditions or full syntax.

[render_test.go](../pkg/jsontemplate/render_test.go) contains conformance vectors
for porting this contract to other lmcode hosts. The current schema registration
lives in lmmock; promoting json_render into lmcode's standard schema and SDKs is
a separate step. There is no Java or ANTLR dependency.

## Data is not the interface schema

Response data is an ordinary lmscript object. The script constructs it when it
runs, serializes it with json_stringify, and passes the value into lmcode with
the existing interpolation mechanism:

~~~text
response = {message: env("LMMOCK_GREETING") || "hello", items: []};

p = _lmcode_ schema="schema.json";
@message
fn message(x: Exchange) -> void implements Handler {
 let body = json_parse(${json_stringify(response)});
 let value = json_parse(json_quote(x.query("text")));
 body = json_with(body, "/message", value);
 x.json(body.to_json());
}
route("GET", "/message", "message");
end _lmcode_
savedsl(p, ARGV[0]);
~~~

With --script, lmmock runs lmscript at server startup. The runtime-created value
becomes a constant in that instance of the lmcode program. It is not recomputed
by lmscript on every HTTP request. Handlers can still fill fields from each
request and read live configuration through config().

With --dsl, the value was captured when the bytecode was saved. Go loads that
program without lmscript installed. Changing an environment variable after
saving bytecode does not change its captured data.

Neither mode requires rebuilding the Go executable when response data changes.
A static JSON asset is also a valid source of data; lmscript is useful for
computing and composing it, not a requirement just to read JSON. This prototype
uses captured values and needs no sidecar asset loader.

schema.json is different: it describes host types, signatures and interfaces.
It contains no response data. lmmock currently maintains that file explicitly;
it is not generated through Go reflection. The compiler uses it for checking
calls, and the Go loader verifies the program against the host contract.

## Structured updates

The new host function is:

~~~text
json_with(document: Json, pointer: string, value: Json) -> Json
~~~

It returns a new document and leaves the input unchanged. The value is structured
JSON, not a string-replacement pattern. A string can be made with
json_parse(json_quote(text)); an existing Json subtree can be passed directly.

Pointers use JSON Pointer syntax:

- /message sets an object field, adding the final field if absent.
- /items/0 replaces an existing array element.
- /items/- appends an array element.
- /a~1b/~0key addresses keys containing slash and tilde.
- An empty pointer replaces the root.

Intermediate containers must exist. Invalid pointers, invalid escapes, missing
parents and out-of-range/noncanonical array indexes fail. The helper preserves
large integer lexemes and does not route numbers through float64. Request values
containing quotes, newlines, backslashes or interpolation-looking text remain
ordinary JSON data.

This is a generic lmmock host extension, not yet a new lmcode language builtin.
The existing lmscript runtime/interpolation and lmcode Json type needed no fork.
If this pattern proves useful in other hosts, promote the operation to the
lmcode standard JSON API across SDKs and expose it in the compiler's standard
schema. Generating the host schema from registration metadata would independently
remove the current signature duplication.

## LLM refactor and compatibility

The LLM fixture defines named objects for responses, tool blocks, errors and SSE
event bodies in lmscript. Handlers render response envelopes with a nested context map, update other
typed fields as needed, and serialize at the HTTP boundary. Tool arguments that must be JSON strings on the wire are constructed
with lmscript json_stringify, not handwritten escaping. SSE framing remains text;
its JSON event bodies use the same objects. SSE remains buffered.

The updated host schema is lmmock_v2. The host also accepts the frozen
schema-v1.json contract so previously compiled fixtures continue to run.
New scripts calling json_render or json_with require the new host; an older binary cannot run
them. Do not copy the new schema into a client repo while leaving its lmmock
binary pinned to an older release.

Validation includes the full 60-scenario SDK suite, template paths, escaping, JSON types, JSON Pointer and numeric
edge cases, an old-schema compiled handler, and a real --script startup test.
The startup test supplies an environment value, sends escaped request data, and
checks that a later request still sees the original immutable template.
