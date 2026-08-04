package nikkei225jp

// InputError は、利用者が修正できる225225.jp取得入力の失敗を表します。
type InputError struct {
	message string
}

// Error は、公開可能な入力エラーメッセージを返します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - string: 利用者へ提示できる日本語メッセージ。
func (e *InputError) Error() string {
	return e.message
}

// newInputError は、225225.jpクライアント内部の入力エラーを生成します。
//
// 引数:
//   - message string: 利用者へ提示できる日本語メッセージ。
//
// 返り値:
//   - error: errors.Asで*InputErrorとして識別できるエラー。
func newInputError(message string) error {
	return &InputError{message: message}
}
