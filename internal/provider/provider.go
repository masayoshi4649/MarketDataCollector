// Package provider は、市場データ提供元を共通サービスへ接続する契約を定義します。
package provider

import (
	"context"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
)

// Collector は、1つのデータ提供元から要求時に情報を収集する機能を表します。
//
// 主な特徴:
//   - transportや保存先を知らず、取得と正規化だけを担当する
//   - DescriptorとCollectのdataset識別子を一致させる
type Collector interface {
	Descriptor() domain.ProviderDescriptor
	Collect(context.Context, string, map[string]any) (domain.ProviderResult, error)
}
