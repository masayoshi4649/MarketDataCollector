// Package domain は、接続方式に依存しない市場データ収集の入出力を定義します。
package domain

import "time"

const APIVersion = "v1"

// ParameterDescriptor は、データセットが受け付ける入力項目を表します。
//
// 主な特徴:
//   - REST APIとMCPのdatalistで同じ説明を返す
//   - provider固有の入力を共通形式で発見可能にする
type ParameterDescriptor struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Description string   `json:"description"`
	Allowed     []string `json:"allowed,omitempty"`
	Default     any      `json:"default,omitempty"`
}

// DatasetDescriptor は、収集可能な1種類の情報と入力仕様を表します。
//
// 主な特徴:
//   - Nameはcollect要求のdatasetと一致する安定識別子
//   - Parametersはprovider固有の入力仕様を表す
type DatasetDescriptor struct {
	Name        string                `json:"name"`
	Description string                `json:"description"`
	Parameters  []ParameterDescriptor `json:"parameters"`
}

// ProviderDescriptor は、データ提供元と対応データセットを表します。
//
// 主な特徴:
//   - datalistへ掲載されたproviderは収集に利用できる
//   - Datasetsはproviderが対応する収集仕様を表す
type ProviderDescriptor struct {
	Name        string              `json:"name"`
	DisplayName string              `json:"display_name"`
	Description string              `json:"description"`
	Datasets    []DatasetDescriptor `json:"datasets"`
}

// DataList は、全接続方式で共有するデータセット一覧です。
//
// 主な特徴:
//   - RESTの/api/datalistとMCPのdatalistが同じ値を返す
//   - Versionで将来の破壊的変更を識別する
type DataList struct {
	Version   string               `json:"version"`
	Providers []ProviderDescriptor `json:"providers"`
}

// CollectRequest は、要求時に収集するデータを指定します。
//
// 主な特徴:
//   - ProviderとDatasetはdatalistに掲載された識別子を使う
//   - Parametersはデータセット固有のJSONオブジェクトを保持する
type CollectRequest struct {
	Provider   string         `json:"provider" jsonschema:"データ提供元の識別子"`
	Dataset    string         `json:"dataset" jsonschema:"収集するデータセットの識別子"`
	Parameters map[string]any `json:"parameters,omitempty" jsonschema:"データセット固有の入力項目"`
}

// ProviderResult は、providerが返す値と付帯情報を保持します。
//
// 主な特徴:
//   - Dataはprovider固有の正規化済みJSON値を保持する
//   - Metadataは利用ライブラリなど収集結果の解釈に必要な情報を保持する
type ProviderResult struct {
	Data     any            `json:"data"`
	Metadata map[string]any `json:"metadata,omitempty"`
}

// CollectResponse は、共通の収集結果です。
//
// 主な特徴:
//   - RESTとMCPで同じフィールドを返す
//   - CollectedAtはprovider処理が正常終了した時刻をUTCで保持する
type CollectResponse struct {
	Version     string         `json:"version"`
	Provider    string         `json:"provider"`
	Dataset     string         `json:"dataset"`
	CollectedAt time.Time      `json:"collected_at"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	Data        any            `json:"data"`
}
