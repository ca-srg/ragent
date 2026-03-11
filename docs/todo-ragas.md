# RAGAS Integration Plan

RAGent に RAGAS (https://github.com/vibrantlabsai/ragas) を導入し、RAG の性能を定量評価・可視化する計画。

## Architecture Decision

**方針: Go は JSONL export のみ、評価は独立 Python パイプラインで batch 実行**

Go CLI 本体に Python 依存を持ち込まず、JSONL ファイルを介した疎結合アーキテクチャとする。

```
RAGent (Go)                              evaluation/ (Python)
┌──────────────────────┐                ┌───────────────────────┐
│ chat / query command │                │ RAGAS evaluation      │
│ --export-eval flag   │──── JSONL ────>│ @experiment decorator │
│                      │                │ + retriever metrics   │
└──────────────────────┘                └───────┬───────────────┘
                                                │
                                           CSV / HTML report
```

### Why This Approach

- 既存パイプライン (`RunQuery` / `GenerateChatResponse` / `HybridSearchService.Search`) をそのまま通すので、評価専用経路との乖離が生じない
- JSONL は Go で追記しやすく、Python でも読みやすく、1 レコード破損しても全体が死なない
- Python 依存を `evaluation/` に隔離することで、Go CLI のビルド・配布・保守が汚れない
- RAGAS v0.4.3 は `evaluate()` を deprecated にし `@experiment` decorator を推奨しており、batch 評価がファーストクラス

---

## Phase 1 — Go 側のデータ収集基盤

### Goal

既存の `chat` / `query` コマンドに `--export-eval` フラグを追加し、各リクエストの入出力を JSONL でエクスポートする。

### Export JSONL Schema

```jsonc
{
  // Identification
  "id": "uuid-v4",
  "timestamp": "2026-03-10T13:00:00Z",
  "command": "chat",           // "chat" | "query"

  // RAGAS required fields (mapped to RAGAS column names)
  "user_input": "OpenSearchのインデックス設定は？",
  "retrieved_contexts": [
    "OpenSearch uses kuromoji tokenizer for Japanese text...",
    "Index settings include: number_of_shards: 1..."
  ],
  "response": "OpenSearchのインデックスは以下のように設定します...",

  // Retriever detail (for Hit@k / MRR / nDCG computation)
  "retrieved_docs": [
    {
      "doc_id": "doc-abc123",
      "rank": 1,
      "text": "...",
      "fused_score": 0.85,
      "bm25_score": 0.70,
      "vector_score": 0.92,
      "search_type": "both",
      "source_file": "docs/opensearch.md",
      "title": "OpenSearch Configuration"
    }
  ],

  // Run configuration (for reproducibility)
  "run_config": {
    "search_mode": "hybrid",
    "bm25_weight": 0.5,
    "vector_weight": 0.5,
    "fusion_method": "weighted_sum",
    "top_k": 10,
    "index_name": "kiberag-documents",
    "use_japanese_nlp": true,
    "chat_model": "anthropic.claude-3-5-sonnet-...",
    "embedding_model": "amazon.titan-embed-text-v2:0",
    "slack_search_enabled": false
  },

  // Timing
  "timing": {
    "total_ms": 1234,
    "embedding_ms": 150,
    "bm25_ms": 200,
    "vector_ms": 300,
    "fusion_ms": 10,
    "llm_ms": 574
  },

  // References (for traceability)
  "references": {
    "OpenSearch Configuration": "https://..."
  }
}
```

### Implementation

| File | Change |
|------|--------|
| `internal/pkg/evalexport/types.go` | Export record struct + JSONL writer |
| `internal/pkg/evalexport/writer.go` | Append-only JSONL writer with file rotation |
| `internal/query/chat_command.go` | `GenerateChatResponse()` 終了後に export record を書き出し |
| `internal/query/query_command.go` | `runHybridSearch()` / `runOpenSearchOnly()` 後に export |
| `cmd/chat.go` / `cmd/query.go` | `--export-eval` / `--export-eval-path` フラグ追加 |

### Flags

```
--export-eval           Enable evaluation data export (default: false)
--export-eval-path      Output path for JSONL (default: ./evaluation/exports/)
```

### Notes

- `internal/pkg/evalexport` に閉じ込め、他パッケージへの影響を最小化
- Slack メッセージや機密文書断片を含む export は注意が必要（Phase 2 で sanitization を検討）
- `v1` では新しい `evaluate` サブコマンドは作らず、既存コマンドをそのまま通す

---

## Phase 2 — Ground Truth Dataset 作成

### Goal

20-50 件の representative な Q&A ペアを手動で作成する。

### Dataset Format (JSONL)

```jsonc
// evaluation/datasets/golden_qa.jsonl
{
  "case_id": "case-001",
  "user_input": "OpenSearchのインデックス設定は？",
  "reference": "OpenSearch indexes are configured with kuromoji tokenizer for Japanese text analysis. Settings include number_of_shards: 1, and the mapping uses dense_vector fields with 1024 dimensions for Titan v2 embeddings.",
  "relevant_doc_ids": ["docs/opensearch.md#index-settings", "docs/vectorize.md#embedding"],
  "tags": ["opensearch", "configuration", "japanese"],
  "difficulty": "easy"
}
```

### Dataset Structure

```
evaluation/datasets/
├── golden_qa.jsonl           # Main evaluation dataset (20-50 cases)
├── golden_qa_retriever.jsonl # Retriever-only evaluation (doc_id relevance labels)
└── README.md                 # Dataset documentation & annotation guidelines
```

### Guidelines

- 機密データや実 Slack メッセージは含めない（必要なら private dataset に分離）
- `reference` は必ず検索対象ドキュメントの内容に基づく
- `relevant_doc_ids` は `context_precision` / `context_recall` に使用
- 日本語クエリを主体とし、一部英語クエリも含める
- カテゴリを分散させる: config, search, vectorize, slackbot, mcp-server

---

## Phase 3 — Python 評価パイプライン

### Goal

RAGAS を使った batch 評価パイプラインを `evaluation/` ディレクトリに構築する。

### Directory Structure

```
evaluation/
├── pyproject.toml            # Python project config (uv/pip)
├── evaluate.py               # Main evaluation script
├── metrics/
│   └── retriever.py          # Hit@k, MRR, nDCG (RAGAS外)
├── datasets/
│   ├── golden_qa.jsonl       # Ground truth
│   └── README.md
├── exports/                  # RAGent が出力した JSONL (gitignored)
├── results/                  # 評価結果 (gitignored)
│   ├── *.csv
│   └── *.html
└── .gitignore
```

### Dependencies

```toml
[project]
name = "ragent-evaluation"
requires-python = ">=3.9"
dependencies = [
    "ragas>=0.4.3",
    "litellm",          # Bedrock provider
    "langchain-aws",    # Alternative Bedrock provider
    "pandas",
    "jinja2",           # HTML report generation
]
```

### Metrics

#### RAGAS Metrics (LLM-as-judge)

| Metric | Required Fields | Purpose |
|--------|----------------|---------|
| `Faithfulness` | user_input, response, retrieved_contexts | 回答が contexts に忠実か |
| `AnswerRelevancy` | user_input, response | 回答がクエリに関連しているか |
| `ContextPrecision` | user_input, retrieved_contexts, reference | 上位 context の関連性 |
| `ContextRecall` | user_input, retrieved_contexts, reference | ground truth の情報が contexts に含まれるか |

#### Retriever Metrics (non-RAGAS, computed from scores)

| Metric | Purpose |
|--------|---------|
| `Hit@k` (k=1,3,5,10) | top-k に正解ドキュメントが含まれるか |
| `MRR` (Mean Reciprocal Rank) | 正解ドキュメントの平均順位 |
| `nDCG@k` | ランク付き検索品質 |

Retriever metrics は hybrid search の改善に不可欠。生成品質だけでなく retriever 単体の劣化検知に使う。

### Bedrock Integration

```python
from ragas.llms import llm_factory

# Option A: LiteLLM (recommended for simplicity)
llm = llm_factory(
    "bedrock/anthropic.claude-3-5-sonnet-20241022-v2:0",
    provider="litellm"
)

# Option B: LangChain AWS (alternative)
from langchain_aws import ChatBedrock
from ragas.llms import LangchainLLMWrapper
bedrock_llm = ChatBedrock(model_id="anthropic.claude-3-5-sonnet-...", region_name="us-west-2")
llm = LangchainLLMWrapper(bedrock_llm)
```

### RAGAS API Note

RAGAS v0.4.x は `evaluate()` を deprecated にし `@experiment` decorator を推奨している。
新規実装では `@experiment` ベースで構築する。

```python
from ragas import experiment, Dataset, SingleTurnSample
from ragas.metrics import Faithfulness, AnswerRelevancy, ContextPrecision, ContextRecall

@experiment(metrics=[Faithfulness(), AnswerRelevancy(), ContextPrecision(), ContextRecall()])
async def evaluate_ragent(sample):
    # Load from JSONL export + golden dataset
    return SingleTurnSample(
        user_input=sample["user_input"],
        retrieved_contexts=sample["retrieved_contexts"],
        response=sample["response"],
        reference=sample["reference"],
    )
```

### Japanese Language Handling

- 「翻訳して評価」ではなく、日本語のまま judge LLM に流す
- まず 10-20 件を人手確認し、faithfulness / answer_relevancy のスコア感が人間評価と大きくズレないか検証
- ズレが大きい場合は RAGAS の prompt adaptation (`adapt_metrics_to_language`) を検討

---

## Phase 4 — Makefile & CI 統合

### Makefile Targets

```makefile
# Run RAGent with eval export enabled
eval-export:
	./RAGent chat --export-eval --export-eval-path ./evaluation/exports/

# Run batch evaluation against golden dataset
eval:
	cd evaluation && uv run python evaluate.py

# Generate HTML report from latest results
eval-report:
	cd evaluation && uv run python report.py

# Full pipeline: export → evaluate → report
eval-full: eval-export eval eval-report
```

### CI (Optional, Phase 4b)

```yaml
# .github/workflows/evaluation.yml
# Nightly or on-demand evaluation against golden dataset
# Requires: AWS credentials, OpenSearch access
```

---

## Effort Estimate

| Phase | Effort | Dependency |
|-------|--------|-----------|
| Phase 1: Go eval export | 0.5-1 day | None |
| Phase 2: Golden dataset | 0.5-1 day | Domain knowledge |
| Phase 3: Python pipeline | 0.5-1 day | Phase 1 + 2 |
| Phase 4: Makefile & CI | 0.5 day | Phase 3 |
| **Total** | **2-3.5 days** | |

---

## Risks & Mitigations

| Risk | Impact | Mitigation |
|------|--------|-----------|
| Export に機密文書断片が含まれる | Data leak | Sanitization 機能を追加、.gitignore で exports/ を除外 |
| 日本語での LLM-as-judge 精度 | Unreliable scores | 10-20件の人手検証で calibration |
| `context_precision` / `context_recall` に doc ID ラベルが必要 | Labeling cost | Phase 2 で `relevant_doc_ids` を丁寧に作成 |
| index 内容・fusion 設定・モデル変更でスコア差分の原因が追えない | Debugging difficulty | `run_config` を JSONL に必ず含める |
| RAGAS の Bedrock 対応が LiteLLM 経由で間接的 | Integration complexity | langchain-aws をフォールバックに用意 |

## Future Enhancements (v2+)

- `RAGent evaluate` サブコマンド: golden dataset を食わせてワンコマンドで評価実行
- Docker 化: Python evaluator 側のみコンテナ化 (CI 再現性)
- 継続的評価: 本番トラフィックの raw trace 収集 → SQLite trace store
- テストデータ自動生成: RAGAS の `TestsetGenerator` で合成テストデータ生成
- Grafana ダッシュボード: OTel metrics として評価スコアを export
- AG-UI 連携: RAGent を HTTP/SSE endpoint として公開し、RAGAS から live 評価
