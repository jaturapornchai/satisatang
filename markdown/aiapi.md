# AI API - Gemini Proxy

Full Gemini API proxy deployed on Vercel. Supports 100% of Gemini API features.

## 🚀 API Endpoint

```
POST https://aiapi-2t4ecfkxh-jaturapornchais-projects.vercel.app/api/chat
```

---

## 📦 Request Parameters

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `message` | string | ⭐ | Simple mode - ข้อความเดียว |
| `contents` | array | ⭐ | Full mode - Gemini contents array |
| `model` | string | ❌ | Model name (default: `gemini-2.5-flash-lite`) |
| `systemInstruction` | object | ❌ | System instruction |
| `generationConfig` | object | ❌ | Temperature, maxTokens, topP, topK, etc. |
| `safetySettings` | array | ❌ | Safety settings |
| `tools` | array | ❌ | Function calling tools |
| `toolConfig` | object | ❌ | Tool configuration |

⭐ = ต้องมีอย่างน้อย 1 อย่าง (`message` หรือ `contents`)

---

## 📝 Usage Examples

### 1️⃣ Simple Mode (Backwards Compatible)

```bash
curl -X POST https://aiapi-2t4ecfkxh-jaturapornchais-projects.vercel.app/api/chat \
  -H "Content-Type: application/json" \
  -d '{"message": "สวัสดี"}'
```

### 2️⃣ Full Mode with System Instruction

```bash
curl -X POST https://aiapi-2t4ecfkxh-jaturapornchais-projects.vercel.app/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-flash-lite",
    "systemInstruction": {
      "parts": [{"text": "You are a helpful assistant. Always respond in Thai."}]
    },
    "contents": [
      {"role": "user", "parts": [{"text": "What is the weather?"}]}
    ]
  }'
```

### 3️⃣ With Generation Config

```bash
curl -X POST https://aiapi-2t4ecfkxh-jaturapornchais-projects.vercel.app/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "message": "Write a poem",
    "generationConfig": {
      "temperature": 0.9,
      "maxOutputTokens": 500,
      "topP": 0.95,
      "topK": 40
    }
  }'
```

### 4️⃣ Multi-turn Conversation

```bash
curl -X POST https://aiapi-2t4ecfkxh-jaturapornchais-projects.vercel.app/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [
      {"role": "user", "parts": [{"text": "My name is John"}]},
      {"role": "model", "parts": [{"text": "Nice to meet you, John!"}]},
      {"role": "user", "parts": [{"text": "What is my name?"}]}
    ]
  }'
```

### 5️⃣ Change Model

```bash
curl -X POST https://aiapi-2t4ecfkxh-jaturapornchais-projects.vercel.app/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "model": "gemini-2.5-flash",
    "message": "Hello"
  }'
```

---

## 📤 Response

Returns **full Gemini API response**:

```json
{
  "candidates": [{
    "content": {
      "parts": [{"text": "Response text here"}],
      "role": "model"
    },
    "finishReason": "STOP",
    "index": 0
  }],
  "modelVersion": "gemini-2.5-flash-lite",
  "usageMetadata": {
    "candidatesTokenCount": 10,
    "promptTokenCount": 5,
    "totalTokenCount": 15
  }
}
```

---

## 🔧 JavaScript Example

```javascript
const response = await fetch('https://aiapi-2t4ecfkxh-jaturapornchais-projects.vercel.app/api/chat', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    systemInstruction: {
      parts: [{ text: 'You are a helpful assistant' }]
    },
    contents: [
      { role: 'user', parts: [{ text: 'Hello!' }] }
    ],
    generationConfig: {
      temperature: 0.7
    }
  })
});

const data = await response.json();
console.log(data.candidates[0].content.parts[0].text);
```

---

## 🐍 Python Example

```python
import requests

response = requests.post(
    'https://aiapi-2t4ecfkxh-jaturapornchais-projects.vercel.app/api/chat',
    json={
        'systemInstruction': {
            'parts': [{'text': 'You are a helpful assistant'}]
        },
        'contents': [
            {'role': 'user', 'parts': [{'text': 'Hello!'}]}
        ],
        'generationConfig': {
            'temperature': 0.7
        }
    }
)

data = response.json()
print(data['candidates'][0]['content']['parts'][0]['text'])
```

---

## 📌 Available Models

| Model | Description |
|-------|-------------|
| `gemini-2.5-flash-lite` | Fastest, lowest cost (default) |
| `gemini-2.5-flash` | Fast, balanced |
| `gemini-2.5-pro` | Most capable |
| `gemini-2.0-flash` | Previous gen fast |

---

## 🖼️ Image Support

Supports sending images via base64 encoding.

**Supported formats**: `image/jpeg`, `image/png`, `image/gif`, `image/webp`

### cURL with Image

```bash
curl -X POST https://aiapi-2t4ecfkxh-jaturapornchais-projects.vercel.app/api/chat \
  -H "Content-Type: application/json" \
  -d '{
    "contents": [{
      "role": "user",
      "parts": [
        {"text": "What is in this image?"},
        {
          "inlineData": {
            "mimeType": "image/jpeg",
            "data": "BASE64_ENCODED_IMAGE_HERE"
          }
        }
      ]
    }]
  }'
```

### JavaScript with Image

```javascript
// Convert image to base64
async function analyzeImage(imageUrl) {
  const imageBuffer = await fetch(imageUrl).then(r => r.arrayBuffer());
  const base64 = btoa(String.fromCharCode(...new Uint8Array(imageBuffer)));

  const response = await fetch('https://aiapi-2t4ecfkxh-jaturapornchais-projects.vercel.app/api/chat', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      contents: [{
        role: 'user',
        parts: [
          { text: 'Describe this image in detail' },
          { inlineData: { mimeType: 'image/jpeg', data: base64 } }
        ]
      }]
    })
  });

  const data = await response.json();
  return data.candidates[0].content.parts[0].text;
}
```

### Python with Image

```python
import requests
import base64

# Read and encode image
with open('image.jpg', 'rb') as f:
    image_base64 = base64.b64encode(f.read()).decode('utf-8')

response = requests.post(
    'https://aiapi-2t4ecfkxh-jaturapornchais-projects.vercel.app/api/chat',
    json={
        'contents': [{
            'role': 'user',
            'parts': [
                {'text': 'What is in this image?'},
                {'inlineData': {'mimeType': 'image/jpeg', 'data': image_base64}}
            ]
        }]
    }
)

data = response.json()
print(data['candidates'][0]['content']['parts'][0]['text'])
```

### Node.js with Image (from file)

```javascript
const fs = require('fs');

const imageBuffer = fs.readFileSync('image.jpg');
const base64 = imageBuffer.toString('base64');

fetch('https://aiapi-2t4ecfkxh-jaturapornchais-projects.vercel.app/api/chat', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    contents: [{
      role: 'user',
      parts: [
        { text: 'Analyze this image' },
        { inlineData: { mimeType: 'image/jpeg', data: base64 } }
      ]
    }]
  })
})
.then(r => r.json())
.then(data => console.log(data.candidates[0].content.parts[0].text));
```

---

## 🖼️ Simplified Image Endpoint (NEW!)

A simpler endpoint specifically for image processing. Perfect for Line OA webhooks and receipt scanning.

### Endpoint

```
POST https://aiapi-2t4ecfkxh-jaturapornchais-projects.vercel.app/api/image
```

### Request Parameters

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `image_base64` | string | ⭐ | Base64-encoded image data |
| `image_url` | string | ⭐ | URL to fetch image from |
| `mime_type` | string | ❌ | Image MIME type (default: `image/jpeg`) |
| `prompt` | string | ❌ | Text prompt (default: "What is in this image?") |
| `system_prompt` | string | ❌ | System instruction for the model |
| `model` | string | ❌ | Model name (default: `gemini-2.5-flash-lite`) |

⭐ = ต้องมีอย่างน้อย 1 อย่าง (`image_base64` หรือ `image_url`)

### Response

Returns simplified response:

```json
{
  "text": "The response text from Gemini"
}
```

Or on error:

```json
{
  "error": "Error message"
}
```

### Example: With Base64

```bash
curl -X POST https://aiapi-2t4ecfkxh-jaturapornchais-projects.vercel.app/api/image \
  -H "Content-Type: application/json" \
  -d '{
    "image_base64": "BASE64_ENCODED_IMAGE_HERE",
    "prompt": "Extract transaction details from this receipt"
  }'
```

### Example: With URL

```bash
curl -X POST https://aiapi-2t4ecfkxh-jaturapornchais-projects.vercel.app/api/image \
  -H "Content-Type: application/json" \
  -d '{
    "image_url": "https://example.com/receipt.jpg",
    "prompt": "Analyze this receipt and extract: store name, date, items, total",
    "system_prompt": "You are a receipt analyzer. Return JSON with structured data."
  }'
```

### Go Example (Line OA Webhook)

```go
type ImageRequest struct {
    ImageURL     string `json:"image_url"`
    Prompt       string `json:"prompt"`
    SystemPrompt string `json:"system_prompt"`
}

type ImageResponse struct {
    Text  string `json:"text"`
    Error string `json:"error"`
}

func processReceiptImage(imageURL string) (string, error) {
    reqBody := ImageRequest{
        ImageURL: imageURL,
        Prompt:   "Extract transaction details from this receipt as JSON",
        SystemPrompt: "You are a receipt analyzer. Extract and return JSON with: store, date, items, total",
    }
    
    jsonData, _ := json.Marshal(reqBody)
    
    resp, err := http.Post(
        "https://aiapi-2t4ecfkxh-jaturapornchais-projects.vercel.app/api/image",
        "application/json",
        bytes.NewBuffer(jsonData),
    )
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()
    
    var result ImageResponse
    json.NewDecoder(resp.Body).Decode(&result)
    
    if result.Error != "" {
        return "", fmt.Errorf(result.Error)
    }
    
    return result.Text, nil
}
```

---

## 🔢 Embedding Endpoint (Vector Search)

Generate text embeddings for semantic search using MongoDB Atlas Vector Search.

### Endpoint

```
POST https://aiapi-e4y6ekwr1-jaturapornchais-projects.vercel.app/api/embed
```

### Request Parameters

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `text` | string | ⭐ | Text to generate embedding for |

### Response

```json
{
  "embedding": [0.123, -0.456, 0.789, ...],  // 768-dimensional vector
  "model": "text-embedding-004"
}
```

### Example: cURL

```bash
curl -X POST https://aiapi-e4y6ekwr1-jaturapornchais-projects.vercel.app/api/embed \
  -H "Content-Type: application/json" \
  -d '{"text": "กินข้าว 50 บาท หมวดอาหาร"}'
```

### Example: Go

```go
type EmbeddingRequest struct {
    Text string `json:"text"`
}

type EmbeddingResponse struct {
    Embedding []float32 `json:"embedding"`
    Error     string    `json:"error,omitempty"`
}

func generateEmbedding(text string) ([]float32, error) {
    reqBody := EmbeddingRequest{Text: text}
    jsonData, _ := json.Marshal(reqBody)

    resp, err := http.Post(
        "https://aiapi-e4y6ekwr1-jaturapornchais-projects.vercel.app/api/embed",
        "application/json",
        bytes.NewBuffer(jsonData),
    )
    if err != nil {
        return nil, err
    }
    defer resp.Body.Close()

    var result EmbeddingResponse
    json.NewDecoder(resp.Body).Decode(&result)

    return result.Embedding, nil
}
```

---

## ⚠️ Notes

- ✅ CORS enabled - call from browser
- ✅ 100% Gemini API compatible
- ✅ Supports function calling, multi-turn, system instructions
- ✅ Returns full Gemini response with usage metadata
- ✅ **NEW:** Simplified `/api/image` endpoint for easy image processing
- ✅ **NEW:** `/api/embed` endpoint for vector embeddings

