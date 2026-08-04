package nikkei225jp

import (
	"context"
	"os"
	"testing"
	"time"
)

// TestLiveMarketEndpoints は、225225.jpの現在の内部配信を直列取得して互換性を確認します。
//
// 引数:
//   - t *testing.T: テスト状態と失敗を管理する値。
//
// 返り値:
//   - なし。LIVE_225225が1でない通常テストではスキップする。
func TestLiveMarketEndpoints(t *testing.T) {
	if os.Getenv("LIVE_225225") != "1" {
		t.Skip("LIVE_225225=1の場合だけ実サイトへ接続します")
	}

	client, err := NewClient(Config{})
	if err != nil {
		t.Fatalf("Clientを生成できません: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	currentSections := []MarketSection{
		MarketSectionTop,
		MarketSectionNikkeiFutures,
		MarketSectionJapan,
		MarketSectionUS,
		MarketSectionEurope,
		MarketSectionAsia,
		MarketSectionCommodities,
		MarketSectionFX,
		MarketSectionBitcoin,
	}
	for _, section := range currentSections {
		data, fetchErr := client.FetchMarketCurrent(ctx, section, nil)
		if fetchErr != nil {
			t.Errorf("%s現在値を取得できません: %v", section, fetchErr)
			continue
		}
		if len(data.Quotes) == 0 {
			t.Errorf("%s現在値が空です", section)
		}
	}

	// ----------------------------------------

	for _, section := range currentSections {
		data, fetchErr := client.FetchMarketChart(ctx, section, MarketChartRangeIntraday, nil)
		if fetchErr != nil {
			t.Errorf("%s短期チャートを取得できません: %v", section, fetchErr)
			continue
		}
		if len(data.Series) == 0 {
			t.Errorf("%s短期チャート系列が空です", section)
		}
	}

	// ----------------------------------------

	if data, _, fetchErr := client.FetchJapanComponents(ctx); fetchErr != nil {
		t.Errorf("日経225構成銘柄を取得できません: %v", fetchErr)
	} else if len(data.Components) == 0 {
		t.Error("日経225構成銘柄が空です")
	}
	if data, _, fetchErr := client.FetchJapanContributions(ctx); fetchErr != nil {
		t.Errorf("日経225寄与度を取得できません: %v", fetchErr)
	} else if len(data.Top)+len(data.Bottom) == 0 {
		t.Error("日経225寄与度が空です")
	}
	if data, _, fetchErr := client.FetchJapanIndustries(ctx); fetchErr != nil {
		t.Errorf("東証33業種を取得できません: %v", fetchErr)
	} else if len(data.Industries) == 0 {
		t.Error("東証33業種が空です")
	}
	if data, _, fetchErr := client.FetchMarketRankings(ctx, MarketSectionJapan); fetchErr != nil {
		t.Errorf("日本株ランキングを取得できません: %v", fetchErr)
	} else if len(data.Gainers)+len(data.Losers)+len(data.Active) == 0 {
		t.Error("日本株ランキングが空です")
	}
	if data, _, fetchErr := client.FetchUSEquities(ctx, USMarketSessionRegular); fetchErr != nil {
		t.Errorf("米国主要銘柄を取得できません: %v", fetchErr)
	} else if len(data.Equities) == 0 {
		t.Error("米国主要銘柄が空です")
	}
	if data, _, fetchErr := client.FetchUSIndustries(ctx); fetchErr != nil {
		t.Errorf("米国業種指数を取得できません: %v", fetchErr)
	} else if len(data.Industries) == 0 {
		t.Error("米国業種指数が空です")
	}
	if data, _, fetchErr := client.FetchMarketRankings(ctx, MarketSectionUS); fetchErr != nil {
		t.Errorf("米国株ランキングを取得できません: %v", fetchErr)
	} else if len(data.Gainers)+len(data.Losers)+len(data.Active) == 0 {
		t.Error("米国株ランキングが空です")
	}
	if data, _, fetchErr := client.FetchADR(ctx); fetchErr != nil {
		t.Errorf("ADR・PTS一覧を取得できません: %v", fetchErr)
	} else if len(data.Quotes) == 0 {
		t.Error("ADR・PTS一覧が空です")
	}
	if data, _, fetchErr := client.FetchFXRates(ctx, nil); fetchErr != nil {
		t.Errorf("為替レート表を取得できません: %v", fetchErr)
	} else if len(data) == 0 {
		t.Error("為替レート表が空です")
	}
	if data, _, fetchErr := client.FetchCryptoAssets(ctx); fetchErr != nil {
		t.Errorf("暗号資産一覧を取得できません: %v", fetchErr)
	} else if len(data.Assets) == 0 {
		t.Error("暗号資産一覧が空です")
	}
}
