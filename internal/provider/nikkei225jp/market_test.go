package nikkei225jp

import (
	"slices"
	"testing"
)

// TestMarketSections は、全対象ページと追加データセットを公開することを検証します。
//
// 引数:
//   - t *testing.T: テスト状態と失敗を管理する値。
//
// 返り値:
//   - なし。期待と異なる場合はテストを失敗させる。
func TestMarketSections(t *testing.T) {
	t.Parallel()

	sections := MarketSections()
	if len(sections) != len(marketSectionOrder) {
		t.Fatalf("市場分類数 = %d、期待値 = %d", len(sections), len(marketSectionOrder))
	}
	for index, section := range sections {
		if section.Section != marketSectionOrder[index] {
			t.Fatalf("市場分類[%d] = %q、期待値 = %q", index, section.Section, marketSectionOrder[index])
		}
		if section.PageURL == "" {
			t.Fatalf("%sのページURLが空です", section.Section)
		}
	}

	adr, err := MarketSectionInformation(MarketSectionADR)
	if err != nil {
		t.Fatalf("ADR分類を取得できません: %v", err)
	}
	if adr.CurrentAvailable {
		t.Fatal("ADR分類を現在値配信ありとして公開しています")
	}
	if !slices.Contains(adr.Datasets, "adr") {
		t.Fatalf("ADRデータセットがありません: %#v", adr.Datasets)
	}
}

// TestMarketSectionsIncludesSingleIntradayCodes は、複合表にない個別チャートもカタログへ含めることを検証します。
//
// 引数:
//   - t *testing.T: テスト状態と失敗を管理する値。
//
// 返り値:
//   - なし。期待と異なる場合はテストを失敗させる。
func TestMarketSectionsIncludesSingleIntradayCodes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		section MarketSection
		codes   []string
	}{
		{section: MarketSectionTop, codes: []string{"151", "643", "811"}},
		{section: MarketSectionCommodities, codes: []string{"111", "921", "931"}},
		{section: MarketSectionFX, codes: []string{"1251", "509", "743"}},
		{section: MarketSectionBitcoin, codes: []string{"1002", "1191", "1381"}},
	}
	for _, test := range tests {
		info, err := MarketSectionInformation(test.section)
		if err != nil {
			t.Fatalf("%sの分類情報を取得できません: %v", test.section, err)
		}
		for _, code := range test.codes {
			if !slices.Contains(info.IntradayCodes, code) {
				t.Errorf("%sの短期コードに%sがありません: %#v", test.section, code, info.IntradayCodes)
			}
		}
		if len(info.IntradayCompositeCodes) == 0 || len(info.IntradaySingleOnlyCodes) == 0 {
			t.Errorf("%sの複合・単一専用コード分類がありません: %#v", test.section, info)
		}
	}
}

// TestMarketSectionInformationReturnsClone は、公開スライスの変更が内部定義へ影響しないことを検証します。
//
// 引数:
//   - t *testing.T: テスト状態と失敗を管理する値。
//
// 返り値:
//   - なし。期待と異なる場合はテストを失敗させる。
func TestMarketSectionInformationReturnsClone(t *testing.T) {
	t.Parallel()

	first, err := MarketSectionInformation(MarketSectionJapan)
	if err != nil {
		t.Fatalf("日本市場分類を取得できません: %v", err)
	}
	first.IntradayCodes[0] = "破損"
	first.HistoryCodes[0] = "破損"
	first.Datasets[0] = "破損"

	second, err := MarketSectionInformation(MarketSectionJapan)
	if err != nil {
		t.Fatalf("日本市場分類を再取得できません: %v", err)
	}
	if second.IntradayCodes[0] == "破損" ||
		second.HistoryCodes[0] == "破損" ||
		second.Datasets[0] == "破損" {
		t.Fatal("公開スライスの変更が内部定義へ反映されました")
	}
}

// TestMarketSectionInformationRejectsUnknown は、未知の分類を通信前に拒否することを検証します。
//
// 引数:
//   - t *testing.T: テスト状態と失敗を管理する値。
//
// 返り値:
//   - なし。エラーがない場合はテストを失敗させる。
func TestMarketSectionInformationRejectsUnknown(t *testing.T) {
	t.Parallel()

	if _, err := MarketSectionInformation(MarketSection("unknown")); err == nil {
		t.Fatal("未知の市場分類が拒否されませんでした")
	}
}
