#!/bin/bash
set -euo pipefail

# RAGent OpenSearch Index Initialization
# Ensures the OpenSearch index exists with correct knn_vector mapping
# before any RAGent service starts.

echo "ragent-init: OpenSearch endpoint=${opensearch_endpoint} index=${opensearch_index_name} dimension=${embedding_dimension}"

%{ if is_docker_opensearch ~}
# Docker mode: wait for OpenSearch container to be healthy
echo "ragent-init: Waiting for Docker OpenSearch to be ready..."
for attempt in $$(seq 1 90); do
  if curl -sf "${opensearch_endpoint}/_cluster/health" > /dev/null 2>&1; then
    echo "ragent-init: OpenSearch is ready (after $${attempt} attempts)"
    break
  fi
  if [ "$$attempt" -eq 90 ]; then
    echo "ragent-init: ERROR - OpenSearch did not become ready within 180s"
    exit 1
  fi
  sleep 2
done
%{ endif ~}

# Create index with proper knn_vector mapping (idempotent)
# If index already exists, OpenSearch returns 400 which we ignore.
HTTP_CODE=$$(curl -s -o /tmp/ragent-init-response.json -w "%%{http_code}" \
  -X PUT "${opensearch_endpoint}/${opensearch_index_name}" \
  -H 'Content-Type: application/json' \
  -d '{
  "settings": {
    "index": {
      "knn": true,
      "number_of_shards": 1,
      "number_of_replicas": 0,
      "max_result_window": 10000,
      "max_rescore_window": 10000
    },
    "analysis": {
      "analyzer": {
        "kuromoji": {
          "type": "custom",
          "tokenizer": "kuromoji_tokenizer",
          "filter": ["lowercase", "kuromoji_baseform", "kuromoji_part_of_speech", "kuromoji_stemmer", "cjk_width", "stop"]
        },
        "kuromoji_search": {
          "type": "custom",
          "tokenizer": "kuromoji_tokenizer",
          "filter": ["lowercase", "kuromoji_baseform", "kuromoji_stemmer", "cjk_width"]
        }
      }
    }
  },
  "mappings": {
    "properties": {
      "title": {
        "type": "text",
        "analyzer": "kuromoji",
        "search_analyzer": "kuromoji_search",
        "fields": { "raw": { "type": "keyword", "ignore_above": 256 } }
      },
      "content": {
        "type": "text",
        "analyzer": "kuromoji",
        "search_analyzer": "kuromoji_search"
      },
      "content_ja": {
        "type": "text",
        "analyzer": "kuromoji",
        "search_analyzer": "kuromoji_search"
      },
      "body": {
        "type": "text",
        "analyzer": "kuromoji",
        "search_analyzer": "kuromoji_search"
      },
      "category": {
        "type": "keyword",
        "fields": { "text": { "type": "text", "analyzer": "kuromoji" } }
      },
      "tags": {
        "type": "keyword",
        "fields": { "text": { "type": "text", "analyzer": "kuromoji" } }
      },
      "author": {
        "type": "keyword",
        "fields": { "text": { "type": "text", "analyzer": "kuromoji" } }
      },
      "reference": {
        "type": "keyword",
        "fields": { "text": { "type": "text", "analyzer": "kuromoji", "search_analyzer": "kuromoji_search" } }
      },
      "source": { "type": "keyword" },
      "file_path": { "type": "keyword" },
      "word_count": { "type": "integer" },
      "secret": { "type": "boolean" },
      "created_at": { "type": "date" },
      "updated_at": { "type": "date" },
      "indexed_at": { "type": "date" },
      "embedding": {
        "type": "knn_vector",
        "dimension": ${embedding_dimension},
        "method": {
          "engine": "lucene",
          "space_type": "cosinesimil",
          "name": "hnsw",
          "parameters": { "ef_construction": 256, "m": 16 }
        }
      },
      "custom_fields": { "type": "object", "enabled": true }
    }
  }
}')

case "$$HTTP_CODE" in
  200|201)
    echo "ragent-init: Index '${opensearch_index_name}' created with knn_vector mapping (dimension=${embedding_dimension})"
    ;;
  400)
    echo "ragent-init: Index '${opensearch_index_name}' already exists (skipped)"
    ;;
  *)
    echo "ragent-init: WARNING - Index creation returned HTTP $$HTTP_CODE"
    cat /tmp/ragent-init-response.json 2>/dev/null || true
    # Don't fail - let services start and handle errors themselves
    ;;
esac

rm -f /tmp/ragent-init-response.json
echo "ragent-init: Initialization complete"
