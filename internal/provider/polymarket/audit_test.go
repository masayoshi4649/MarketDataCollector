package polymarket

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// TestAuditKeysetMetadataKeepsMissingCursorUnknown は、cursor欠落時の非推測契約を検証します。
//
// 機能:
//   - Gamma keysetは公式の終端時field省略に従い、cursor欠落を既知の終端とする
//   - CLOB keysetは公式のLTE= sentinelが欠落した場合にhas_moreを断定しない
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestAuditKeysetMetadataKeepsMissingCursorUnknown(t *testing.T) {
	gammaTerminal := buildPaginationMetadata(paginationKeyset, "events", nil, map[string]any{"events": []any{}})
	if gammaTerminal["has_more_known"] != true || gammaTerminal["has_more"] != false {
		t.Errorf("Gamma終端metadata = %#v, has_more既知falseを期待", gammaTerminal)
	}

	missing := buildPaginationMetadata(paginationKeyset, "clob_markets", nil, map[string]any{"data": []any{}})
	if missing["has_more_known"] != false {
		t.Errorf("CLOB cursor欠落metadata = %#v, has_more_known=falseを期待", missing)
	}
	for _, name := range []string{"has_more", "next_cursor"} {
		if _, exists := missing[name]; exists {
			t.Errorf("cursor欠落metadataが%qを推測しました: %#v", name, missing)
		}
	}

	terminal := buildPaginationMetadata(paginationKeyset, "clob_markets", nil, map[string]any{"next_cursor": "LTE="})
	if terminal["has_more_known"] != true || terminal["has_more"] != false || terminal["next_cursor"] != "LTE=" {
		t.Errorf("CLOB終端cursor metadata = %#v", terminal)
	}
}

// ----------------------------------------

// TestAuditPacingCancellationAndWorkerRestart は、FIFOのcancelとlifecycleを検証します。
//
// 機能:
//   - 実行中要求の後ろでcancelした未開始要求を通信せずFIFOから除く
//   - 後続要求が追い越しではなく正常に実行されることを確認する
//   - queue空時にworkerが停止し、次のExecuteで再起動できることを確認する
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//
// 返り値:
//   - なし
func TestAuditPacingCancellationAndWorkerRestart(t *testing.T) {
	pacing := newPacingState()
	for class := range pacing.intervals {
		pacing.intervals[class] = 0
	}
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	firstResult := make(chan error, 1)
	go func() {
		_, executeErr := pacing.Execute(context.Background(), []rateClass{rateGammaGeneral}, func(context.Context) (APIResponse, error) {
			close(firstStarted)
			<-firstRelease
			return APIResponse{}, nil
		})
		firstResult <- executeErr
	}()
	select {
	case <-firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("先頭要求が開始されません")
	}

	cancelContext, cancel := context.WithCancel(context.Background())
	var canceledExecuted atomic.Bool
	canceledResult := make(chan error, 1)
	go func() {
		_, executeErr := pacing.Execute(cancelContext, []rateClass{rateDataGeneral}, func(context.Context) (APIResponse, error) {
			canceledExecuted.Store(true)
			return APIResponse{}, nil
		})
		canceledResult <- executeErr
	}()
	waitForPacingQueueLength(t, pacing, 1)
	cancel()
	select {
	case executeErr := <-canceledResult:
		if !errors.Is(executeErr, context.Canceled) {
			t.Errorf("cancel要求error = %v, context.Canceledを期待", executeErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("cancel要求が終了しません")
	}

	thirdStarted := make(chan struct{})
	thirdResult := make(chan error, 1)
	go func() {
		_, executeErr := pacing.Execute(context.Background(), []rateClass{rateCLOBGeneral}, func(context.Context) (APIResponse, error) {
			close(thirdStarted)
			return APIResponse{}, nil
		})
		thirdResult <- executeErr
	}()
	waitForPacingQueueLength(t, pacing, 1)
	close(firstRelease)
	for _, result := range []<-chan error{firstResult, thirdResult} {
		select {
		case executeErr := <-result:
			if executeErr != nil {
				t.Errorf("Execute() error = %v", executeErr)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("FIFO要求が終了しません")
		}
	}
	select {
	case <-thirdStarted:
	default:
		t.Error("後続要求が実行されていません")
	}
	if canceledExecuted.Load() {
		t.Error("cancel済みの未開始要求が実行されました")
	}
	waitForPacingWorkerState(t, pacing, false)

	_, err := pacing.Execute(context.Background(), []rateClass{rateGammaGeneral}, func(context.Context) (APIResponse, error) {
		return APIResponse{}, nil
	})
	if err != nil {
		t.Fatalf("worker再起動後のExecute() error = %v", err)
	}
	waitForPacingWorkerState(t, pacing, false)
}

// ----------------------------------------

// waitForPacingWorkerState は、FIFO workerが指定状態になるまで待機します。
//
// 機能:
//   - pacing mutex下でrunningを読み取る
//   - テスト全体のハングを防ぐ2秒の期限を設ける
//
// 引数:
//   - t *testing.T: テスト状態と失敗内容を管理する値
//   - pacing *pacingState: 確認対象のFIFO状態
//   - want bool: 期待するworker実行状態
//
// 返り値:
//   - なし
func waitForPacingWorkerState(t *testing.T, pacing *pacingState, want bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pacing.mu.Lock()
		actual := pacing.running
		pacing.mu.Unlock()
		if actual == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("pacing workerが実行状態%tになりません", want)
}
