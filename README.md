# llmp

Simple LLM proxy for OpenAI/Claude-compatible models with token auth. Uses `Authorization` header for Anthropic instead of `x-api-key`.

Supports LiteLLM-compatible config.

## Run

```bash
docker run -e LITELLM_MASTER_KEY="sk-1234" -dp 4000:8400 -v /etc/litellm/config.yaml:/app/config.yaml local/llmp /app/config.yaml
```

## Build

```bash
make install-ko
make build-local
```

## Use

```bash
 curl -X POST 127.1:4000/v1/chat/completions \
-H "Authorization: Bearer sk-1234" \
-H "Content-Type: application/json" \
-d '{
    "model": " gpt-4o",
    "max_tokens": 1024,
    "messages": [{
            "role": "user",
            "content": "林黛玉和韦小宝啥关系？" } ],
    "stream": true
}'
```