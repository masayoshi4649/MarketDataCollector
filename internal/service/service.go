// Package service は、REST APIとMCPが共有する収集処理を提供します。
package service

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/masayoshi4649/MarketDataCollector/internal/domain"
	"github.com/masayoshi4649/MarketDataCollector/internal/provider"
)

// Service は、provider登録と共通の入力検証を管理します。
type Service struct {
	providers   map[string]provider.Collector
	descriptors []domain.ProviderDescriptor
}

// New は、重複とデータセット定義を検証して共通サービスを生成します。
//
// 引数:
//   - collectors []provider.Collector: 起動設定により登録対象となったprovider一覧。
//
// 返り値:
//   - *Service: REST APIとMCPから共有できる収集サービス。
//   - error: providerまたはdataset定義が不正な場合のエラー。
func New(collectors []provider.Collector) (*Service, error) {
	result := &Service{
		providers:   make(map[string]provider.Collector, len(collectors)),
		descriptors: make([]domain.ProviderDescriptor, 0, len(collectors)),
	}
	for index, collector := range collectors {
		if isNilCollector(collector) {
			return nil, fmt.Errorf("provider %dがnilです", index+1)
		}
		descriptor := collector.Descriptor()
		if err := validateProviderDescriptor(descriptor); err != nil {
			return nil, fmt.Errorf("provider %dの定義が不正です: %w", index+1, err)
		}
		if _, exists := result.providers[descriptor.Name]; exists {
			return nil, fmt.Errorf("provider %qが重複しています", descriptor.Name)
		}
		result.providers[descriptor.Name] = collector
		result.descriptors = append(result.descriptors, cloneProviderDescriptor(descriptor))
	}
	return result, nil
}

// ----------------------------------------

// isNilCollector は、interfaceへ格納された型付きnilを含めて検出します。
//
// 引数:
//   - collector provider.Collector: 起動時に登録するprovider実装。
//
// 返り値:
//   - bool: collectorがnilまたは型付きnilの場合はtrue。
func isNilCollector(collector provider.Collector) bool {
	if collector == nil {
		return true
	}
	value := reflect.ValueOf(collector)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

// ----------------------------------------

// DataList は、通信を行わず全providerの固定仕様を返します。
//
// 引数:
//   - なし。
//
// 返り値:
//   - domain.DataList: 呼び出し側と内部定義がスライスを共有しない一覧。
func (s *Service) DataList() domain.DataList {
	providers := make([]domain.ProviderDescriptor, len(s.descriptors))
	for index, descriptor := range s.descriptors {
		providers[index] = cloneProviderDescriptor(descriptor)
	}
	return domain.DataList{Version: domain.APIVersion, Providers: providers}
}

// Collect は、providerとdatasetを検証して要求時収集を実行します。
//
// 引数:
//   - ctx context.Context: 期限とキャンセルを伝える要求コンテキスト。
//   - request domain.CollectRequest: provider、dataset、固有入力を含む要求。
//
// 返り値:
//   - domain.CollectResponse: providerの結果へ共通識別子と完了時刻を付けた値。
//   - error: 入力、未知provider、上流取得などの失敗。
func (s *Service) Collect(ctx context.Context, request domain.CollectRequest) (domain.CollectResponse, error) {
	request.Provider = strings.TrimSpace(request.Provider)
	request.Dataset = strings.TrimSpace(request.Dataset)
	if request.Provider == "" {
		return domain.CollectResponse{}, domain.NewServiceError(
			domain.ErrorInvalidArgument,
			"providerを指定してください",
			nil,
		)
	}
	if request.Dataset == "" {
		return domain.CollectResponse{}, domain.NewServiceError(
			domain.ErrorInvalidArgument,
			"datasetを指定してください",
			nil,
		)
	}

	collector, exists := s.providers[request.Provider]
	if !exists {
		return domain.CollectResponse{}, domain.NewServiceError(
			domain.ErrorNotFound,
			fmt.Sprintf("provider %qは存在しません", request.Provider),
			nil,
		)
	}
	descriptor := collector.Descriptor()
	if !containsDataset(descriptor.Datasets, request.Dataset) {
		return domain.CollectResponse{}, domain.NewServiceError(
			domain.ErrorNotFound,
			fmt.Sprintf("provider %qにdataset %qはありません", request.Provider, request.Dataset),
			nil,
		)
	}
	if request.Parameters == nil {
		request.Parameters = map[string]any{}
	}

	providerResult, err := collector.Collect(ctx, request.Dataset, request.Parameters)
	if err != nil {
		var serviceErr *domain.ServiceError
		if errors.As(err, &serviceErr) {
			return domain.CollectResponse{}, err
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return domain.CollectResponse{}, domain.NewServiceError(
				domain.ErrorTimeout,
				fmt.Sprintf("provider %qの収集処理が制限時間を超えました", request.Provider),
				err,
			)
		}
		return domain.CollectResponse{}, domain.NewServiceError(
			domain.ErrorUpstream,
			fmt.Sprintf("provider %qからdataset %qを収集できません", request.Provider, request.Dataset),
			err,
		)
	}
	return domain.CollectResponse{
		Version:     domain.APIVersion,
		Provider:    request.Provider,
		Dataset:     request.Dataset,
		CollectedAt: time.Now().UTC(),
		Metadata:    providerResult.Metadata,
		Data:        providerResult.Data,
	}, nil
}

// ----------------------------------------

// validateProviderDescriptor は、providerの公開情報、識別子、dataset重複を確認します。
//
// 引数:
//   - descriptor domain.ProviderDescriptor: 起動時に登録するprovider定義。
//
// 返り値:
//   - error: 表示名・概要の不足、不正な識別子、dataset重複を検出した場合のエラー。
func validateProviderDescriptor(descriptor domain.ProviderDescriptor) error {
	if err := validateIdentifier(descriptor.Name, "provider名"); err != nil {
		return err
	}
	if strings.TrimSpace(descriptor.DisplayName) == "" {
		return errors.New("provider表示名が空です")
	}
	if strings.TrimSpace(descriptor.Description) == "" {
		return errors.New("provider概要が空です")
	}
	if len(descriptor.Datasets) == 0 {
		return errors.New("datasetが1件もありません")
	}
	seen := make(map[string]struct{}, len(descriptor.Datasets))
	for _, dataset := range descriptor.Datasets {
		if err := validateIdentifier(dataset.Name, "dataset名"); err != nil {
			return err
		}
		if _, exists := seen[dataset.Name]; exists {
			return fmt.Errorf("dataset %qが重複しています", dataset.Name)
		}
		seen[dataset.Name] = struct{}{}
		parameterNames := make(map[string]struct{}, len(dataset.Parameters))
		for _, parameter := range dataset.Parameters {
			if err := validateIdentifier(parameter.Name, "parameter名"); err != nil {
				return fmt.Errorf("dataset %q: %w", dataset.Name, err)
			}
			if _, exists := parameterNames[parameter.Name]; exists {
				return fmt.Errorf("dataset %qのparameter %qが重複しています", dataset.Name, parameter.Name)
			}
			parameterNames[parameter.Name] = struct{}{}
		}
	}
	return nil
}

// validateIdentifier は、API識別子が到達可能なASCII形式か確認します。
//
// 引数:
//   - value string: provider、dataset、parameterの識別子。
//   - fieldName string: エラー表示に使う項目名。
//
// 返り値:
//   - error: 空、前後空白、英小文字・数字・アンダースコア・ハイフン以外を含む場合のエラー。
func validateIdentifier(value string, fieldName string) error {
	if value == "" {
		return fmt.Errorf("%sが空です", fieldName)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%sの前後に空白を含めることはできません: %q", fieldName, value)
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') || character == '_' || character == '-' {
			continue
		}
		return fmt.Errorf("%sに利用できない文字があります: %q", fieldName, value)
	}
	return nil
}

// containsDataset は、一覧に指定datasetが存在するか確認します。
//
// 引数:
//   - datasets []domain.DatasetDescriptor: providerが公開するdataset一覧。
//   - name string: 検索するdataset識別子。
//
// 返り値:
//   - bool: 完全一致するdatasetが存在する場合はtrue。
func containsDataset(datasets []domain.DatasetDescriptor, name string) bool {
	return slices.ContainsFunc(datasets, func(dataset domain.DatasetDescriptor) bool {
		return dataset.Name == name
	})
}

// cloneProviderDescriptor は、公開仕様のスライスを深く複製します。
//
// 引数:
//   - descriptor domain.ProviderDescriptor: 複製元のprovider定義。
//
// 返り値:
//   - domain.ProviderDescriptor: 内部スライスを共有しない複製。
func cloneProviderDescriptor(descriptor domain.ProviderDescriptor) domain.ProviderDescriptor {
	descriptor.Datasets = slices.Clone(descriptor.Datasets)
	for datasetIndex := range descriptor.Datasets {
		dataset := &descriptor.Datasets[datasetIndex]
		dataset.Parameters = slices.Clone(dataset.Parameters)
		for parameterIndex := range dataset.Parameters {
			parameter := &dataset.Parameters[parameterIndex]
			parameter.Allowed = slices.Clone(parameter.Allowed)
		}
	}
	return descriptor
}
