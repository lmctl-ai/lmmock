"""Real Python LiteLLM client acceptance; lifecycle is driven by test.lms."""
import os
# Avoid fetching LiteLLM's model-cost catalog during import.
os.environ["LITELLM_LOCAL_MODEL_COST_MAP"] = "True"
import litellm

base = os.environ["LMMOCK_URL"] + "/v1"
args = dict(model="openai/mock-agent", api_base=base, api_key="fixture-key",
            messages=[{"role": "user", "content": "ping"}], num_retries=0)
result = litellm.completion(**args)
assert result.choices[0].message.content == "pong"
assert result.usage.prompt_tokens == 2
chunks = list(litellm.completion(**args, stream=True))
assert "".join(c.choices[0].delta.content or "" for c in chunks if c.choices) == "pong"
tools = [{"type": "function", "function": {"name": "weather", "parameters": {
    "type": "object", "properties": {"city": {"type": "string"}}, "required": ["city"]}}}]
result = litellm.completion(**args, tools=tools)
call = result.choices[0].message.tool_calls[0]
assert call.function.name == "weather"
history = args["messages"] + [result.choices[0].message.model_dump(exclude_none=True),
    {"role": "tool", "tool_call_id": call.id, "content": "Paris 21C"}]
result = litellm.completion(**{**args, "messages": history}, tools=tools)
assert "Paris" in result.choices[0].message.content
vectors = litellm.embedding(model="openai/mock-embedding", input=["a", "b"],
                           api_base=base, api_key="fixture-key")
assert len(vectors.data) == 2
assert vectors.data[0]["embedding"] == [0.1, 0.2, 0.3]
print("PASS Python LiteLLM chat, stream, tools and embeddings")
