package service

import (
	"context"
	"errors"
	"testing"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
	"github.com/masayoshi4649/MarketDataCollector/internal/provider"
)

type fakeCollector struct {
	descriptor domain.ProviderDescriptor
	result     domain.ProviderResult
	err        error
	requests   []domain.CollectRequest
}

/*
Descriptor は、テストで設定したprovider仕様を返します。

機能:
  - 外部通信せず固定のprovider定義を返す

引数:
  - なし

返り値:
  - domain.ProviderDescriptor: テストで設定したprovider定義
*/
func (f *fakeCollector) Descriptor() domain.ProviderDescriptor {
	return f.descriptor
}

/*
Collect は、要求を記録して設定済み結果を返します。

機能:
  - serviceから渡されたdatasetとparametersをテスト検証用に保存する

引数:
  - ctx context.Context: 要求コンテキスト
  - dataset string: 収集対象のデータセット識別子
  - parameters map[string]any: provider固有入力

返り値:
  - domain.ProviderResult: 設定済みの収集結果
  - error: 設定済みのエラー
*/
func (f *fakeCollector) Collect(
	ctx context.Context,
	dataset string,
	parameters map[string]any,
) (domain.ProviderResult, error) {
	_ = ctx
	f.requests = append(f.requests, domain.CollectRequest{
		Provider: f.descriptor.Name, Dataset: dataset, Parameters: parameters,
	})
	return f.result, f.err
}

// ----------------------------------------

/*
TestServiceSharesCatalogAndCollectionContract は、一覧と収集の共通契約を検証します。

機能:
  - datalistがprovider定義を返すことを確認する
  - collectが同じdataset識別子を使って結果へ共通項目を付けることを確認する
  - datalist返却値の変更が内部定義へ影響しないことを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestServiceSharesCatalogAndCollectionContract(t *testing.T) {
	collector := &fakeCollector{
		descriptor: domain.ProviderDescriptor{
			Name: "test", DisplayName: "テスト",
			Datasets: []domain.DatasetDescriptor{{
				Name: "prices", Description: "価格",
				Parameters: []domain.ParameterDescriptor{{Name: "symbol", Allowed: []string{"A"}}},
			}},
		},
		result: domain.ProviderResult{
			Data:     map[string]any{"price": 123},
			Metadata: map[string]any{"source": "fake"},
		},
	}
	service, err := New([]provider.Collector{collector})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	dataList := service.DataList()
	if dataList.Version != domain.APIVersion || len(dataList.Providers) != 1 {
		t.Fatalf("DataList() = %+v, provider 1件を期待", dataList)
	}
	dataList.Providers[0].Datasets[0].Parameters[0].Allowed[0] = "変更"
	if service.DataList().Providers[0].Datasets[0].Parameters[0].Allowed[0] != "A" {
		t.Error("DataList()の返却スライスが内部定義と共有されています")
	}

	result, err := service.Collect(context.Background(), domain.CollectRequest{
		Provider: " test ", Dataset: " prices ", Parameters: map[string]any{"symbol": "A"},
	})
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if result.Provider != "test" || result.Dataset != "prices" || result.CollectedAt.IsZero() {
		t.Errorf("Collect() = %+v, 共通識別子と完了時刻を期待", result)
	}
	if len(collector.requests) != 1 || collector.requests[0].Parameters["symbol"] != "A" {
		t.Errorf("provider要求 = %+v, 入力引き渡しを期待", collector.requests)
	}
}

// ----------------------------------------

/*
TestServiceRejectsUnknownProviderAndDataset は、未知のproviderとdatasetを検証します。

機能:
  - 未知providerとdatasetをNOT_FOUNDに分類する
  - provider実装を呼び出す前に未知の識別子を拒否する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestServiceRejectsUnknownProviderAndDataset(t *testing.T) {
	collector := &fakeCollector{descriptor: domain.ProviderDescriptor{
		Name: "test", DisplayName: "テスト",
		Datasets: []domain.DatasetDescriptor{{Name: "prices"}},
	}}
	service, err := New([]provider.Collector{collector})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	_, err = service.Collect(context.Background(), domain.CollectRequest{Provider: "missing", Dataset: "prices"})
	assertServiceErrorKind(t, err, domain.ErrorNotFound)
	_, err = service.Collect(context.Background(), domain.CollectRequest{Provider: "test", Dataset: "missing"})
	assertServiceErrorKind(t, err, domain.ErrorNotFound)
	if len(collector.requests) != 0 {
		t.Errorf("未知データのprovider呼び出し回数 = %d, 0を期待", len(collector.requests))
	}
}

// ----------------------------------------

/*
TestServiceAllowsEmptyProviderList は、providerを登録しない起動状態を検証します。

機能:
  - providerが0件でもサービスを生成できることを確認する
  - datalistが空のprovider一覧を返すことを確認する

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestServiceAllowsEmptyProviderList(t *testing.T) {
	service, err := New(nil)
	if err != nil {
		t.Fatalf("New(nil) error = %v", err)
	}
	if providers := service.DataList().Providers; len(providers) != 0 {
		t.Errorf("DataList().Providers = %+v, 空を期待", providers)
	}
}

// ----------------------------------------

/*
TestServiceRejectsTypedNilCollector は、interface内の型付きnilを検証します。

機能:
  - provider.Collectorへ格納されたnilポインターを起動時に拒否する
  - Descriptor呼び出しによるpanicを防ぐ

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestServiceRejectsTypedNilCollector(t *testing.T) {
	var collector *fakeCollector
	_, err := New([]provider.Collector{collector})
	if err == nil {
		t.Fatal("New() error = nil, 型付きnilの拒否を期待")
	}
}

// ----------------------------------------

/*
TestServiceRejectsUnreachableDescriptorIdentifiers は、公開識別子の起動時検証を確認します。

機能:
  - 前後空白を含むproviderとdatasetを拒否する
  - 重複parameterを拒否して到達不能または曖昧な仕様を防ぐ

引数:
  - t *testing.T: テスト状態を管理する値

返り値:
  - なし
*/
func TestServiceRejectsUnreachableDescriptorIdentifiers(t *testing.T) {
	testCases := []domain.ProviderDescriptor{
		{Name: " test", DisplayName: "テスト", Datasets: []domain.DatasetDescriptor{{Name: "data"}}},
		{Name: "test", DisplayName: "テスト", Datasets: []domain.DatasetDescriptor{{Name: "data "}}},
		{Name: "test", DisplayName: "テスト", Datasets: []domain.DatasetDescriptor{{
			Name:       "data",
			Parameters: []domain.ParameterDescriptor{{Name: "code"}, {Name: "code"}},
		}}},
	}
	for _, descriptor := range testCases {
		collector := &fakeCollector{descriptor: descriptor}
		if _, err := New([]provider.Collector{collector}); err == nil {
			t.Errorf("New(%+v) error = nil, 不正識別子の拒否を期待", descriptor)
		}
	}
}

/*
assertServiceErrorKind は、エラーが期待した共通分類か確認します。

機能:
  - errors.AsでServiceErrorを抽出してKindを比較する

引数:
  - t *testing.T: テスト状態を管理する値
  - err error: 確認するエラー
  - kind domain.ErrorKind: 期待する失敗分類

返り値:
  - なし
*/
func assertServiceErrorKind(t *testing.T, err error, kind domain.ErrorKind) {
	t.Helper()
	var serviceErr *domain.ServiceError
	if !errors.As(err, &serviceErr) || serviceErr.Kind != kind {
		t.Fatalf("error = %v, kind=%sを期待", err, kind)
	}
}
