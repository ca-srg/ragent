package filter

import (
	"encoding/json"
	"fmt"

	"github.com/ca-srg/mdrag/internal/types"
)

// BuildExclusionFilter は除外カテゴリを元にS3 Vector用のフィルタを構築します
func BuildExclusionFilter(cfg *types.Config, userFilter map[string]interface{}) (map[string]interface{}, error) {
	if len(cfg.ExcludeCategories) == 0 && len(userFilter) == 0 {
		return nil, nil
	}

	filter := make(map[string]interface{})

	// ユーザー指定のフィルタがある場合はそれをベースにする
	if len(userFilter) > 0 {
		for k, v := range userFilter {
			filter[k] = v
		}
	}

	// 除外カテゴリがある場合は追加
	if len(cfg.ExcludeCategories) > 0 {
		// S3 Vectorのフィルタ構文: {"category": {"$nin": ["個人メモ", "日報"]}}
		excludeFilter := map[string]interface{}{
			"$nin": cfg.ExcludeCategories,
		}

		// 既存のcategoryフィルタがある場合は統合する必要がある
		if existingCategory, exists := filter["category"]; exists {
			// 既存フィルタと統合
			return mergeFilters(filter, "category", excludeFilter, existingCategory)
		} else {
			filter["category"] = excludeFilter
		}
	}

	return filter, nil
}

// BuildExclusionFilterFromJSON はJSONフィルタクエリと除外カテゴリを統合します
func BuildExclusionFilterFromJSON(cfg *types.Config, filterQuery string) (map[string]interface{}, error) {
	var userFilter map[string]interface{}

	// JSONフィルタクエリをパース
	if filterQuery != "" {
		if err := json.Unmarshal([]byte(filterQuery), &userFilter); err != nil {
			return nil, fmt.Errorf("invalid filter JSON: %w", err)
		}
	}

	return BuildExclusionFilter(cfg, userFilter)
}

// mergeFilters は既存のカテゴリフィルタと除外フィルタを統合します
func mergeFilters(filter map[string]interface{}, key string, excludeFilter map[string]interface{}, existingFilter interface{}) (map[string]interface{}, error) {
	// 複雑な統合ロジックが必要な場合はここで処理
	// 現在は単純に除外フィルタで上書き
	filter[key] = excludeFilter
	return filter, nil
}
