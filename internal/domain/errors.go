package domain

import "fmt"

// ErrorKind は、transport間で共有する失敗分類を表します。
type ErrorKind string

const (
	ErrorInvalidArgument     ErrorKind = "INVALID_ARGUMENT"
	ErrorNotFound            ErrorKind = "NOT_FOUND"
	ErrorProviderUnavailable ErrorKind = "PROVIDER_UNAVAILABLE"
	ErrorUpstream            ErrorKind = "UPSTREAM_ERROR"
	ErrorTimeout             ErrorKind = "TIMEOUT"
	ErrorInternal            ErrorKind = "INTERNAL"
)

// ServiceError は、公開可能なエラー分類とメッセージを保持します。
//
// 主な特徴:
//   - KindはRESTの状態コードとMCPのtool errorへ共通利用する
//   - Causeはログやerrors.Isに利用し、公開応答には含めない
type ServiceError struct {
	Kind    ErrorKind
	Message string
	Cause   error
}

// Error は、利用者へ公開可能なメッセージを返します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - string: エラー分類と公開メッセージを結合した文字列。
func (e *ServiceError) Error() string {
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

// Unwrap は、内部原因を標準エラーチェーンへ公開します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - error: 保持している内部原因。原因がない場合はnil。
func (e *ServiceError) Unwrap() error {
	return e.Cause
}

// NewServiceError は、共通エラーを生成します。
//
// 引数:
//   - kind ErrorKind: transportが判定に使う失敗分類。
//   - message string: 利用者へ公開できる日本語メッセージ。
//   - cause error: ログとエラーチェーンだけに利用する内部原因。
//
// 返り値:
//   - *ServiceError: 指定内容を保持した共通エラー。
func NewServiceError(kind ErrorKind, message string, cause error) *ServiceError {
	return &ServiceError{Kind: kind, Message: message, Cause: cause}
}
