# Draft: GitHub データソース追加

## Requirements (confirmed)
- GitHub リポジトリを clone/fetch し、.md / .csv ファイルを再帰的に取得してベクトル化する
- `vectorize` コマンドに統合、`follow` モード対応
- 複数リポジトリ対応、カンマ区切り単一フラグ `--github-repos "owner1/repo1,owner2/repo2"`
- GitHub Token は環境変数 `GITHUB_TOKEN` のみ（パブリック/プライベート両対応）
- メタデータ自動設定:
  - Owner 名 → Author フィールドに設定
  - リポジトリ名 → Source フィールドに設定
  - カテゴリ: ファイルの直上ディレクトリ名（最下層のみ）。例: `docs/v2/bank/account/overview.md` → Category: "account"
  - Reference: GitHub 上の URL を自動設定

## Technical Decisions (confirmed)
1. **カテゴリ起点**: リポジトリルートから全階層を見るが、Category には最下層のみを設定
2. **Category 格納方式**: 既存の string 型を維持、最下層のディレクトリ名のみ
3. **フラグ形式**: `--github-repos "owner1/repo1,owner2/repo2"` カンマ区切り単一フラグ
4. **Git 実装**: go-git ライブラリ（Pure Go、外部依存なし）
5. **Clone 先**: os.TempDir() 配下、処理完了後にクリーンアップ
6. **SourceType**: "github" を新規追加（hashstore でも使用）
7. **認証**: `GITHUB_TOKEN` 環境変数、未設定時はパブリックリポジトリのみ対応

## Research Findings (from code investigation)
### 既存パイプライン構造
- **Scanner**: `internal/scanner/scanner.go` (ローカル), `s3_scanner.go` (S3)
- **Metadata Extractor**: `internal/metadata/extractor.go` - FrontMatter or ファイルパスからCategory抽出
- **Types**: `internal/types/types.go`
  - `FileInfo`: Path, Name, Size, ModTime, Content, ContentHash, SourceType ("local"|"s3")
  - `DocumentMetadata`: Title, Category(string), Tags([]string), Author, Reference, Source, FilePath
- **Vectorize Command**: `cmd/vectorize.go`
  - ローカルとS3の2ソースを統合済み
  - follow モード実装済み（interval, IPC server）
  - hashstore による差分検出
- **既存パターン**: S3 ソースは `--enable-s3 --s3-bucket xxx` フラグで有効化

### 重要な設計ポイント
- Category は `string` 型（単一値）→ 最下層のみで既存互換性維持
- FileInfo.SourceType で "local" / "s3" を区別 → "github" 追加は自然
- hashstore パッケージで変分検出 → sourceType "github" 対応必要
- S3Scanner と同じインターフェース（ScanBucket相当 + DownloadFile相当）に揃えると統合しやすい

## Scope Boundaries
- INCLUDE: GitHub リポジトリ clone/fetch、.md/.csv スキャン、メタデータ自動生成（owner/repo/category）、vectorize 統合、follow モード対応、hashstore 対応
- EXCLUDE: GitHub Actions/webhook による自動トリガー、GitHub API 経由のファイル取得（go-git使用）、PR/Issue の取り込み、ブランチ選択（デフォルトブランチのみ）
