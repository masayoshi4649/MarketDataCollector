package strictjson

import (
	"encoding/json"
	"testing"
)

type testInput struct {
	Name  string      `json:"name"`
	Value json.Number `json:"value"`
}

/*
TestDecodeObject は、厳密なJSONオブジェクト復号を検証します。

機能:
  - 正しい小文字キーと大きな整数を保持する
  - 大文字違い、重複キー、余分なJSON値を拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestDecodeObject(t *testing.T) {
	var input testInput
	if err := DecodeObject([]byte(`{"name":"x","value":9007199254740991}`), &input, "name", "value"); err != nil {
		t.Fatalf("DecodeObject() error = %v", err)
	}
	if input.Name != "x" || input.Value.String() != "9007199254740991" {
		t.Errorf("復号結果 = %+v", input)
	}

	for _, body := range []string{
		`{"Name":"x","value":1}`,
		`{"name":"x","name":"y","value":1}`,
		`{"name":"x","value":1} {}`,
		`[]`,
	} {
		if err := DecodeObject([]byte(body), &testInput{}, "name", "value"); err == nil {
			t.Errorf("DecodeObject(%s) error = nil, 厳密な拒否を期待", body)
		}
	}
	invalidUTF8 := []byte(`{"name":"`)
	invalidUTF8 = append(invalidUTF8, 0xff)
	invalidUTF8 = append(invalidUTF8, []byte(`","value":1}`)...)
	if err := DecodeObject(invalidUTF8, &testInput{}, "name", "value"); err == nil {
		t.Error("DecodeObject(不正UTF-8) error = nil, 拒否を期待")
	}
}
