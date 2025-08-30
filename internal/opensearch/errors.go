package opensearch

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ca-srg/kiberag/internal/types"
)

type SearchError struct {
	Type       types.ErrorType `json:"type"`
	Message    string          `json:"message"`
	StatusCode int             `json:"status_code,omitempty"`
	Retryable  bool            `json:"retryable"`
	RetryAfter time.Duration   `json:"retry_after,omitempty"`
	Query      string          `json:"query,omitempty"`
	Suggestion string          `json:"suggestion,omitempty"`
	Timestamp  time.Time       `json:"timestamp"`
}

func (e *SearchError) Error() string {
	if e.StatusCode > 0 {
		return fmt.Sprintf("[%s] %s (HTTP %d)", e.Type, e.Message, e.StatusCode)
	}
	return fmt.Sprintf("[%s] %s", e.Type, e.Message)
}

func (e *SearchError) IsRetryable() bool {
	return e.Retryable
}

func NewSearchError(errType types.ErrorType, message string) *SearchError {
	return &SearchError{
		Type:      errType,
		Message:   message,
		Retryable: false,
		Timestamp: time.Now(),
	}
}

func NewRetryableSearchError(errType types.ErrorType, message string, retryAfter time.Duration) *SearchError {
	return &SearchError{
		Type:       errType,
		Message:    message,
		Retryable:  true,
		RetryAfter: retryAfter,
		Timestamp:  time.Now(),
	}
}

func ClassifyHTTPError(statusCode int, body string) *SearchError {
	switch statusCode {
	case http.StatusUnauthorized:
		return &SearchError{
			Type:       types.ErrorTypeValidation,
			Message:    "認証に失敗しました。OpenSearchの認証情報を確認してください。",
			StatusCode: statusCode,
			Retryable:  false,
			Suggestion: "AWS認証情報が正しく設定されているか確認してください。",
			Timestamp:  time.Now(),
		}
	case http.StatusForbidden:
		return &SearchError{
			Type:       types.ErrorTypeValidation,
			Message:    "アクセスが拒否されました。IAM権限を確認してください。",
			StatusCode: statusCode,
			Retryable:  false,
			Suggestion: "IAMロールにOpenSearchへのアクセス権限があることを確認してください。",
			Timestamp:  time.Now(),
		}
	case http.StatusNotFound:
		return &SearchError{
			Type:       types.ErrorTypeValidation,
			Message:    "指定されたインデックスまたはエンドポイントが見つかりません。",
			StatusCode: statusCode,
			Retryable:  false,
			Suggestion: "OpenSearchエンドポイントURLとインデックス名を確認してください。",
			Timestamp:  time.Now(),
		}
	case http.StatusRequestTimeout:
		return &SearchError{
			Type:       types.ErrorTypeNetworkTimeout,
			Message:    "リクエストがタイムアウトしました。",
			StatusCode: statusCode,
			Retryable:  true,
			RetryAfter: 5 * time.Second,
			Suggestion: "ネットワーク接続またはOpenSearchクラスターの負荷を確認してください。",
			Timestamp:  time.Now(),
		}
	case http.StatusTooManyRequests:
		retryAfter := 10 * time.Second
		if strings.Contains(body, "retry after") {
			retryAfter = 30 * time.Second
		}
		return &SearchError{
			Type:       types.ErrorTypeRateLimit,
			Message:    "レート制限に達しました。しばらくしてから再試行してください。",
			StatusCode: statusCode,
			Retryable:  true,
			RetryAfter: retryAfter,
			Suggestion: "リクエスト頻度を下げるか、レート制限設定を調整してください。",
			Timestamp:  time.Now(),
		}
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable:
		return &SearchError{
			Type:       types.ErrorTypeNetworkTimeout,
			Message:    "OpenSearchサーバーエラーが発生しました。",
			StatusCode: statusCode,
			Retryable:  true,
			RetryAfter: 10 * time.Second,
			Suggestion: "OpenSearchクラスターの状態を確認してください。",
			Timestamp:  time.Now(),
		}
	default:
		return &SearchError{
			Type:       types.ErrorTypeUnknown,
			Message:    fmt.Sprintf("予期しないHTTPエラーが発生しました: %s", body),
			StatusCode: statusCode,
			Retryable:  statusCode >= 500,
			RetryAfter: 5 * time.Second,
			Timestamp:  time.Now(),
		}
	}
}

func ClassifyConnectionError(err error) *SearchError {
	errMsg := err.Error()

	if strings.Contains(errMsg, "timeout") {
		return &SearchError{
			Type:       types.ErrorTypeNetworkTimeout,
			Message:    "OpenSearchへの接続がタイムアウトしました。",
			Retryable:  true,
			RetryAfter: 5 * time.Second,
			Suggestion: "ネットワーク接続とOpenSearchエンドポイントを確認してください。",
			Timestamp:  time.Now(),
		}
	}

	if strings.Contains(errMsg, "connection refused") {
		return &SearchError{
			Type:       types.ErrorTypeValidation,
			Message:    "OpenSearchへの接続が拒否されました。",
			Retryable:  false,
			Suggestion: "OpenSearchエンドポイントURLとポートが正しいか確認してください。",
			Timestamp:  time.Now(),
		}
	}

	if strings.Contains(errMsg, "no such host") {
		return &SearchError{
			Type:       types.ErrorTypeValidation,
			Message:    "OpenSearchホストが見つかりません。",
			Retryable:  false,
			Suggestion: "OpenSearchエンドポイントURLのホスト名を確認してください。",
			Timestamp:  time.Now(),
		}
	}

	return &SearchError{
		Type:       types.ErrorTypeUnknown,
		Message:    fmt.Sprintf("接続エラー: %v", err),
		Retryable:  true,
		RetryAfter: 10 * time.Second,
		Suggestion: "ネットワーク接続を確認してください。",
		Timestamp:  time.Now(),
	}
}
