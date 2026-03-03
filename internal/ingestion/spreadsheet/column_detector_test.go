package spreadsheet

import (
	"testing"
)

// Test data based on data.csv structure (sample data for testing)
var testHeaders = []string{
	"#", "BTS#", "PJ", "開発", "起票日", "完了日", "経過", "検知経路", "再現性",
	"フェーズ", "カテゴリ", "トラック", "ランク", "タイトル", "詳細",
	"発生区分1", "発生区分2", "症状区分1", "症状区分2", "進行不可", "固有", "仕様相違", "社外障害", "仕様返答",
}

var testRows = [][]string{
	{
		"1", "BTS-001", "サンプルプロジェクト", "開発チームA", "2024/01/15", "2024/01/16", "1", "内部テスト", "3/3",
		"開発", "Web", "バグ", "A", "ログイン画面でボタンが正しく表示されない問題",
		"ログイン画面において送信ボタンが画面下部に隠れてしまう現象が発生しています。特定のブラウザ幅で再現します。",
		"UI", "表示", "レイアウト", "ボタン", "", "", "", "", "",
	},
	{
		"2", "BTS-002", "サンプルプロジェクト", "開発チームB", "2024/01/20", "2024/02/05", "16", "QAテスト", "2/3",
		"テスト", "API", "機能改善", "B",
		"ユーザー検索APIのレスポンス時間改善",
		`【詳細】
ユーザー検索APIのレスポンス時間が遅い問題について報告します。

現在の状態：平均レスポンス時間 3.5秒
目標：1秒以下

【再現手順】
1. 管理画面にログインする
2. ユーザー検索画面を開く
3. 検索条件を入力して検索を実行する

【調査結果】
データベースクエリの最適化が必要と判断されます。`,
		"パフォーマンス", "検索機能", "速度", "レスポンス", "", "○", "", "", "",
	},
}

func TestDetectContentColumns(t *testing.T) {
	detector := NewColumnDetector(testHeaders, testRows)

	contentColumns := detector.DetectContentColumns()

	if len(contentColumns) == 0 {
		t.Fatal("Expected to detect at least one content column")
	}

	// Should detect "詳細" as it's in the known content column names
	found := false
	for _, col := range contentColumns {
		if col == "詳細" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected to detect '詳細' as content column, got: %v", contentColumns)
	}
}

func TestDetectTitleColumn(t *testing.T) {
	detector := NewColumnDetector(testHeaders, testRows)

	titleColumn := detector.DetectTitleColumn()

	if titleColumn != "タイトル" {
		t.Errorf("Expected 'タイトル', got: %s", titleColumn)
	}
}

func TestDetectCategoryColumn(t *testing.T) {
	detector := NewColumnDetector(testHeaders, testRows)

	categoryColumn := detector.DetectCategoryColumn()

	if categoryColumn != "カテゴリ" {
		t.Errorf("Expected 'カテゴリ', got: %s", categoryColumn)
	}
}

func TestDetectIDColumn(t *testing.T) {
	detector := NewColumnDetector(testHeaders, testRows)

	idColumn := detector.DetectIDColumn()

	if idColumn != "#" {
		t.Errorf("Expected '#', got: %s", idColumn)
	}
}

func TestGetColumnIndex(t *testing.T) {
	detector := NewColumnDetector(testHeaders, testRows)

	tests := []struct {
		columnName string
		expected   int
	}{
		{"#", 0},
		{"タイトル", 13},
		{"詳細", 14},
		{"存在しないカラム", -1},
	}

	for _, tt := range tests {
		t.Run(tt.columnName, func(t *testing.T) {
			idx := detector.GetColumnIndex(tt.columnName)
			if idx != tt.expected {
				t.Errorf("GetColumnIndex(%s) = %d, want %d", tt.columnName, idx, tt.expected)
			}
		})
	}
}

func TestColumnExists(t *testing.T) {
	detector := NewColumnDetector(testHeaders, testRows)

	tests := []struct {
		columnName string
		expected   bool
	}{
		{"#", true},
		{"タイトル", true},
		{"詳細", true},
		{"存在しないカラム", false},
	}

	for _, tt := range tests {
		t.Run(tt.columnName, func(t *testing.T) {
			exists := detector.ColumnExists(tt.columnName)
			if exists != tt.expected {
				t.Errorf("ColumnExists(%s) = %v, want %v", tt.columnName, exists, tt.expected)
			}
		})
	}
}

func TestDetectByAverageLength(t *testing.T) {
	// Test with headers where no known content column names exist
	headers := []string{"col1", "col2", "col3"}
	rows := [][]string{
		{"short", "medium text here", "This is a much longer text that should be detected as content because it has the highest average length"},
		{"a", "some text", "Another long text entry that contributes to a higher average length for this column"},
	}

	detector := NewColumnDetector(headers, rows)
	contentColumns := detector.DetectContentColumns()

	if len(contentColumns) == 0 {
		t.Fatal("Expected to detect at least one content column")
	}

	// Should detect col3 as it has the highest average length
	if contentColumns[0] != "col3" {
		t.Errorf("Expected 'col3' to be detected as content column, got: %v", contentColumns)
	}
}

func TestMatchesAny(t *testing.T) {
	tests := []struct {
		header     string
		knownNames []string
		expected   bool
	}{
		{"詳細", knownContentColumnNames, true},
		{"CONTENT", knownContentColumnNames, true},
		{"content", knownContentColumnNames, true},
		{"Content", knownContentColumnNames, true},
		{"unknown", knownContentColumnNames, false},
		{"タイトル", knownTitleColumnNames, true},
		{"Title", knownTitleColumnNames, true},
		{"カテゴリ", knownCategoryColumnNames, true},
		{"Category", knownCategoryColumnNames, true},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			result := matchesAny(tt.header, tt.knownNames)
			if result != tt.expected {
				t.Errorf("matchesAny(%s) = %v, want %v", tt.header, result, tt.expected)
			}
		})
	}
}

func TestEmptyData(t *testing.T) {
	// Test with empty headers and rows
	detector := NewColumnDetector([]string{}, [][]string{})

	contentColumns := detector.DetectContentColumns()
	if contentColumns != nil {
		t.Errorf("Expected nil for empty data, got: %v", contentColumns)
	}

	titleColumn := detector.DetectTitleColumn()
	if titleColumn != "" {
		t.Errorf("Expected empty string for empty data, got: %s", titleColumn)
	}

	categoryColumn := detector.DetectCategoryColumn()
	if categoryColumn != "" {
		t.Errorf("Expected empty string for empty data, got: %s", categoryColumn)
	}

	idColumn := detector.DetectIDColumn()
	if idColumn != "" {
		t.Errorf("Expected empty string for empty data, got: %s", idColumn)
	}
}
