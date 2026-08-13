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
	Name        string   `json:"name" jsonschema:"入力項目名です。"`
	Type        string   `json:"type" jsonschema:"入力値の型です。"`
	Required    bool     `json:"required" jsonschema:"入力が必須かを表します。"`
	Description string   `json:"description" jsonschema:"入力項目の説明です。"`
	Allowed     []string `json:"allowed,omitempty" jsonschema:"指定可能な値が限定される場合の候補です。"`
	Default     any      `json:"default,omitempty" jsonschema:"省略時の既定値です。"`
	Minimum     *float64 `json:"minimum,omitempty" jsonschema:"数値入力で許可する最小値です。"`
	Maximum     *float64 `json:"maximum,omitempty" jsonschema:"数値入力で許可する最大値です。"`
}

// DatasetDescriptor は、収集可能な1種類の情報と入力仕様を表します。
//
// 主な特徴:
//   - Nameはcollect要求のdatasetと一致する安定識別子
//   - Parametersはprovider固有の入力仕様を表す
type DatasetDescriptor struct {
	Name        string                `json:"name" jsonschema:"collectのdatasetへ指定する安定識別子です。"`
	Description string                `json:"description" jsonschema:"datasetの概要です。"`
	Parameters  []ParameterDescriptor `json:"parameters" jsonschema:"collectのparametersへ指定できる入力項目です。"`
}

// ProviderDescriptor は、データ提供元と対応データセットを表します。
//
// 主な特徴:
//   - datalistへ掲載されたproviderは収集に利用できる
//   - Datasetsはproviderが対応する収集仕様を表す
type ProviderDescriptor struct {
	Name        string              `json:"name" jsonschema:"collectのproviderへ指定する安定識別子です。"`
	DisplayName string              `json:"display_name" jsonschema:"providerの表示名です。"`
	Description string              `json:"description" jsonschema:"providerの概要です。"`
	Datasets    []DatasetDescriptor `json:"datasets" jsonschema:"providerから収集できるdatasetの一覧です。"`
}

// DataList は、全接続方式で共有するデータセット一覧です。
//
// 主な特徴:
//   - RESTの/api/datalistとMCPのdatalistが同じ値を返す
//   - Versionで将来の破壊的変更を識別する
type DataList struct {
	Version   string               `json:"version" jsonschema:"API契約のバージョンです。"`
	Providers []ProviderDescriptor `json:"providers" jsonschema:"現在の設定で利用できるproviderの一覧です。"`
}

// CollectRequest は、要求時に収集するデータを指定します。
//
// 主な特徴:
//   - ProviderとDatasetはdatalistに掲載された識別子を使う
//   - Parametersはデータセット固有のJSONオブジェクトを保持する
type CollectRequest struct {
	Provider   string         `json:"provider" jsonschema:"この会話で一覧を未確認の場合は先にdatalistで全候補を比較して選ぶ、データ提供元の識別子です。"`
	Dataset    string         `json:"dataset" jsonschema:"datalistで選択したproviderに掲載されているデータセットの識別子です。"`
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
	Version     string         `json:"version" jsonschema:"API契約のバージョンです。"`
	Provider    string         `json:"provider" jsonschema:"収集に使用したproviderの識別子です。"`
	Dataset     string         `json:"dataset" jsonschema:"収集したdatasetの識別子です。"`
	CollectedAt time.Time      `json:"collected_at" jsonschema:"provider処理が完了したUTC日時です。"`
	Metadata    map[string]any `json:"metadata,omitempty" jsonschema:"取得元やページングなど、結果の解釈に必要な付帯情報です。"`
	Data        any            `json:"data" jsonschema:"providerとdatasetに固有の市場データです。具体的な形はdatalistと各provider仕様で確認します。"`
}
