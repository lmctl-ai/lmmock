# Runtime JSON data from lmscript to lmcode

See [json-response.lms](../examples/json-response.lms) for the small example and
[llm-api.lms](../examples/llm-api.lms) for the full OpenAI, Anthropic and Gemini mock.

## Standard text templates

lmmock uses lmcode-go's standard function (SDK v0.6.38):

~~~text
template_render(template: string, context: Json) -> string
~~~

There is no lmmock-specific template engine. Templates can contain any text;
JSON is only the container for the context. The portable contract and shared
conformance vectors live in the SDK's `std/template.md` and
`std/template-cases.json`.

- Nested maps: `<user.profile.name>`.
- List indexes: `<items[0].name>`.
- Loops: `<items:{item|<index>:<item.name>}; separator=", ">`.
- Boolean conditions: `<if(user.active)>yes<else>no<endif>`.
- Literal opening bracket: `<<` emits `<`.
- Substitution is one pass. Inserted text containing placeholders stays literal.
- Strings insert as raw text; other values insert as compact JSON.
- Missing paths, wrong types and malformed blocks fail. Only the selected
  conditional branch is evaluated. There are no expressions, includes or macros.

The renderer does not escape JSON, HTML or any other format. For JSON output,
encode string values with `json_quote` before placing them in the context;
objects and lists can be inserted directly. Then use `json_parse` to validate
the rendered text before sending it.

The examples construct native lmscript objects and serialize them with
`json_stringify`. At script execution, they remove the quotes around the
specific whole-token placeholders they own. Handlers supply encoded string
tokens or structured values. This avoids handwritten escaped JSON while keeping
format-specific encoding explicit. Do not remove quotes around arbitrary data.

Angle-bracket placeholders do not overlap with lmscript interpolation. The
small example demonstrates escaped request strings, nested maps and lists;
the LLM fixture uses the same standard function for response envelopes.

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

With --lmcode, the value was captured when the bytecode was saved. Go loads that
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

The updated host schema is lmmock_v3. The host also accepts the frozen
schema-v1.json contract. Recompile v2 fixtures: the former local `json_render`
function has been removed in favor of standard `template_render`. Update the
lmmock binary and schema together. The explicit standard signature in the host
schema also allows compilation with an npm compiler whose bundled catalog has
not yet received the standard function.

Validation includes the full 60-scenario SDK suite, template paths, escaping, JSON types, JSON Pointer and numeric
edge cases, an old-schema compiled handler, and a real --script startup test.
The startup test supplies an environment value, sends escaped request data, and
checks that a later request still sees the original immutable template.
