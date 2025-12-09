# MongoDB Atlas Vector Search Setup

## Overview

Vector Search ใช้สำหรับค้นหาแบบ semantic (ค้นหาตามความหมาย) เช่น:
- "เคยซื้ออะไรที่ 7-11 บ้าง" -> หาทุกรายการที่เกี่ยวกับ 7-11
- "ค่าใช้จ่ายเกี่ยวกับอาหาร" -> หารายการอาหารทั้งหมด รวมทั้งเครื่องดื่ม ขนม

## Prerequisites

1. MongoDB Atlas cluster (M0 Free tier ขึ้นไป)
2. AI API endpoint สำหรับ generate embeddings

## Step 1: Create Embeddings Collection

Collection `embeddings` จะถูกสร้างอัตโนมัติเมื่อ save embedding แรก

Schema:
```json
{
  "_id": ObjectId,
  "lineid": "U...",
  "date": "2024-01-15",
  "text": "รายจ่าย กินข้าว 50 บาท หมวดอาหาร",
  "embedding": [0.123, -0.456, ...],  // 768 dimensions
  "tx_id": ObjectId,
  "type": -1,          // 1=income, -1=expense
  "amount": 50,
  "category": "อาหาร",
  "description": "กินข้าว",
  "created_at": ISODate
}
```

## Step 2: Create Vector Search Index on Atlas

### Option A: Via Atlas UI

1. ไปที่ MongoDB Atlas Console
2. เลือก Cluster -> Browse Collections
3. เลือก Database -> Collection `embeddings`
4. ไปที่ Tab **Search Indexes**
5. คลิก **Create Search Index**
6. เลือก **JSON Editor**
7. ตั้งชื่อ Index: `embedding_index`
8. ใส่ JSON definition:

```json
{
  "fields": [
    {
      "type": "vector",
      "path": "embedding",
      "numDimensions": 768,
      "similarity": "cosine"
    },
    {
      "type": "filter",
      "path": "lineid"
    }
  ]
}
```

9. คลิก **Create Search Index**

### Option B: Via MongoDB Shell

```javascript
db.embeddings.createSearchIndex({
  name: "embedding_index",
  definition: {
    fields: [
      {
        type: "vector",
        path: "embedding",
        numDimensions: 768,
        similarity: "cosine"
      },
      {
        type: "filter",
        path: "lineid"
      }
    ]
  }
});
```

### Option C: Via Atlas Admin API

```bash
curl -X POST \
  -u "PUBLIC_KEY:PRIVATE_KEY" \
  "https://cloud.mongodb.com/api/atlas/v2/groups/{groupId}/clusters/{clusterName}/fts/indexes" \
  -H "Content-Type: application/json" \
  -d '{
    "collectionName": "embeddings",
    "database": "satisatang",
    "name": "embedding_index",
    "definition": {
      "fields": [
        {
          "type": "vector",
          "path": "embedding",
          "numDimensions": 768,
          "similarity": "cosine"
        },
        {
          "type": "filter",
          "path": "lineid"
        }
      ]
    }
  }'
```

## Step 3: Verify Index

```javascript
// In MongoDB Shell
db.embeddings.aggregate([
  { $listSearchIndexes: {} }
])
```

ต้องเห็น index `embedding_index` มี status เป็น `READY`

## Usage in Code

### Save Embedding (เมื่อมี transaction ใหม่)

```go
// Generate embedding text
text := fmt.Sprintf("%s %s %.0f บาท หมวด%s", typeStr, tx.Description, tx.Amount, tx.Category)

// Generate embedding vector
embedding, err := ai.GenerateEmbedding(ctx, text)
if err != nil {
    log.Printf("Failed to generate embedding: %v", err)
    // Continue without embedding - not critical
}

// Save embedding
if embedding != nil {
    mongo.SaveTransactionEmbedding(ctx, lineID, tx, date, embedding)
}
```

### Search (เมื่อผู้ใช้ค้นหา)

```go
// Generate query embedding
queryText := "ค่าอาหารเดือนนี้"
queryVector, err := ai.GenerateEmbedding(ctx, queryText)
if err != nil {
    return nil, err
}

// Vector search
results, err := mongo.VectorSearch(ctx, lineID, queryVector, 10)
if err != nil {
    return nil, err
}

// Format results
resultText := mongo.GetVectorSearchResultText(results)
```

## Index Parameters

| Parameter | Value | Description |
|-----------|-------|-------------|
| `numDimensions` | 768 | ต้องตรงกับ embedding model (text-embedding-004 = 768) |
| `similarity` | cosine | วิธีคำนวณความคล้าย (cosine, euclidean, dotProduct) |
| `filter` | lineid | Field ที่ใช้ filter ก่อน search |

## Query Parameters

| Parameter | Description |
|-----------|-------------|
| `index` | ชื่อ search index |
| `path` | field ที่เก็บ embedding |
| `queryVector` | vector ของ query |
| `numCandidates` | จำนวน candidates สำหรับ approximate search |
| `limit` | จำนวนผลลัพธ์ |
| `filter` | filter เพิ่มเติม (เช่น lineid) |

## Troubleshooting

### Error: "Search index not found"
- ตรวจสอบชื่อ index ตรงกับ `embedding_index`
- รอ index สร้างเสร็จ (status = READY)

### Error: "Vector dimension mismatch"
- ตรวจสอบ `numDimensions` ในIndex ตรงกับ embedding model

### Slow queries
- เพิ่ม `numCandidates` (แต่จะช้าขึ้น)
- พิจารณาใช้ filter เพื่อลดขอบเขตการค้นหา

## Cost Considerations

- M0/M2/M5 clusters: Vector Search รองรับแต่มีข้อจำกัด
- M10+: รองรับ full features
- Embedding API calls: คิดเงินต่อ request
