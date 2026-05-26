curl -N -X POST "{{baseUrl}}/v1/chat/completions" \
  -H "Authorization: Bearer {{authToken}}" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "dashscope_qwen3_coder",
    "messages": [{"role": "user", "content": "Hello, introduce yourself briefly."}],
    "stream": true
  }' >> stream-response.txt