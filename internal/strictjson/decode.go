// Package strictjson は、公開境界のJSONオブジェクトを厳密なキー名で復号します。
package strictjson

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// DecodeObject は、許可したトップレベルキーだけを持つJSONオブジェクトを復号します。
//
// 引数:
//   - data []byte: 1つのJSONオブジェクトを含む入力。
//   - target any: 復号先の構造体ポインター。
//   - allowedKeys ...string: 大文字小文字を区別する許可済みキー名。
//
// 返り値:
//   - error: 非オブジェクト、未知キー、重複キー、型不一致、余分なJSON値を検出した場合のエラー。
func DecodeObject(data []byte, target any, allowedKeys ...string) error {
	if !utf8.Valid(data) {
		return errors.New("JSONは正しいUTF-8で符号化してください")
	}
	allowed := make(map[string]struct{}, len(allowedKeys))
	for _, key := range allowedKeys {
		allowed[key] = struct{}{}
	}

	validator := json.NewDecoder(bytes.NewReader(data))
	validator.UseNumber()
	opening, err := validator.Token()
	if err != nil {
		return err
	}
	if delimiter, ok := opening.(json.Delim); !ok || delimiter != '{' {
		return errors.New("JSON値はオブジェクトにしてください")
	}
	seen := make(map[string]struct{}, len(allowedKeys))
	for validator.More() {
		token, tokenErr := validator.Token()
		if tokenErr != nil {
			return tokenErr
		}
		key, ok := token.(string)
		if !ok {
			return errors.New("JSONオブジェクトのキーが文字列ではありません")
		}
		if _, exists := allowed[key]; !exists {
			return fmt.Errorf("未知のJSON項目です: %q", key)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("JSON項目が重複しています: %q", key)
		}
		seen[key] = struct{}{}
		var raw json.RawMessage
		if decodeErr := validator.Decode(&raw); decodeErr != nil {
			return decodeErr
		}
	}
	if _, err := validator.Token(); err != nil {
		return err
	}
	if err := validator.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("JSONオブジェクトの後ろに余分な値があります")
	}

	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("JSONオブジェクトの後ろに余分な値があります")
	}
	return nil
}
