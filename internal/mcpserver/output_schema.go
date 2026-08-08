package mcpserver

import (
	"reflect"
	"slices"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
)

// outputSchemaFor は、Go型からMCPツールのoutput schemaを生成します。
//
// 主な特徴:
//   - JSON項目名、必須状態、階層を対象型のjsonタグから生成する
//   - 項目説明を対象型のjsonschemaタグから生成する
//   - time.TimeをJSONのdate-time文字列として公開する
//
// 型引数:
//   - T: MCPツールが成功時に返すGo型。
//
// 引数:
//   - description string: output schema全体の説明。
//
// 返り値:
//   - *jsonschema.Schema: JSON Schema Draft 2020-12として利用できるSchema。
//   - error: 型からSchemaを生成できない場合のエラー。
func outputSchemaFor[T any](description string) (*jsonschema.Schema, error) {
	schema, err := jsonschema.For[T](&jsonschema.ForOptions{
		TypeSchemas: map[reflect.Type]*jsonschema.Schema{
			reflect.TypeFor[time.Time](): {
				Type:   "string",
				Format: "date-time",
			},
		},
	})
	if err != nil {
		return nil, err
	}
	schema.Description = description
	requireArrayTypes(schema)
	return schema, nil
}

// requireArrayTypes は、Goのsliceから生成されたSchemaを配列専用に補正します。
//
// 主な特徴:
//   - jsonschema-goがsliceへ付けるnull許可を除去する
//   - 従来から公開している配列専用のoutput schema契約を維持する
//   - object、array、mapの子Schemaを再帰的に処理する
//
// 引数:
//   - schema *jsonschema.Schema: 型から生成された補正対象Schema。
//
// 返り値:
//   - なし。
func requireArrayTypes(schema *jsonschema.Schema) {
	if schema == nil {
		return
	}
	if len(schema.Types) == 2 &&
		slices.Contains(schema.Types, "null") &&
		slices.Contains(schema.Types, "array") {
		schema.Type = "array"
		schema.Types = nil
	}
	for _, propertySchema := range schema.Properties {
		requireArrayTypes(propertySchema)
	}
	requireArrayTypes(schema.Items)
	requireArrayTypes(schema.AdditionalProperties)
}
