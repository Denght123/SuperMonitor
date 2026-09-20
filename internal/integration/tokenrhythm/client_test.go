package tokenrhythm

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeCredential(t *testing.T) {
	credential, err := NormalizeCredential("phone----sess_example----sk_tr_x----rf_tr_x")
	if err != nil || credential.SessionToken != "sess_example" {
		t.Fatalf("unexpected credential: %+v err=%v", credential, err)
	}
	credential, err = NormalizeCredential("tr_ref_device=ref; tr_session=sess_token; other=x")
	if err != nil || credential.SessionToken != "sess_token" || credential.RefDevice != "ref" {
		t.Fatalf("unexpected cookie credential: %+v err=%v", credential, err)
	}
}

func TestFetchTodayUsageParsesOfficialModelAggregates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("range"); got != "today" {
			t.Fatalf("range = %q, want today", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sess_test" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("Cookie"); got != "tr_session=sess_test; tr_ref_device=ref_test" {
			t.Fatalf("cookie = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"code": 0,
			"data": {
				"startAt": "2026-09-20T00:00:00+08:00",
				"timezone": "Asia/Shanghai",
				"summary": {
					"inputTokens": 1200,
					"outputTokens": 340,
					"cacheReadTokens": 500,
					"cacheCreationTokens": 60,
					"calls": 7
				},
				"byModel": [
					{
						"model": "claude-sonnet-4-5",
						"inputTokens": 800,
						"outputTokens": 200,
						"cacheReadTokens": 400,
						"cacheCreationTokens": 40,
						"calls": 4
					},
					{
						"modelId": "gpt-5",
						"inputTokens": "400",
						"outputTokens": 140,
						"cacheReadTokens": 100,
						"cacheCreationTokens": 20,
						"calls": 3
					}
				]
			}
		}`))
	}))
	defer server.Close()

	client := &Client{
		http:          server.Client(),
		usageURL:      server.URL + "/usage-summary",
		usagePanelURL: server.URL + "/usage/panel?range=today&page=1&pageSize=1",
	}
	panel, err := client.FetchTodayUsage(context.Background(), Credential{SessionToken: "sess_test", RefDevice: "ref_test"})
	if err != nil {
		t.Fatal(err)
	}
	if panel.Date != "2026-09-20" || panel.Timezone != "Asia/Shanghai" {
		t.Fatalf("unexpected period: %+v", panel)
	}
	if panel.InputTokens != 1200 || panel.OutputTokens != 340 || panel.CacheReadTokens != 500 || panel.CacheWriteTokens != 60 || panel.Calls != 7 {
		t.Fatalf("unexpected summary: %+v", panel)
	}
	if len(panel.Models) != 2 || panel.Models[0].Model != "claude-sonnet-4-5" || panel.Models[1].Model != "gpt-5" {
		t.Fatalf("unexpected models: %+v", panel.Models)
	}
}

func TestFetchTodayUsageRejectsExpiredSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"code":"UNAUTHORIZED"}`, http.StatusUnauthorized)
	}))
	defer server.Close()
	client := &Client{http: server.Client(), usagePanelURL: server.URL}
	if _, err := client.FetchTodayUsage(context.Background(), Credential{SessionToken: "sess_test"}); err == nil {
		t.Fatal("expected expired session error")
	}
}

func TestFetchUsageHistoryPaginatesDeduplicatesAndAggregatesByBeijingDate(t *testing.T) {
	requestedPages := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("range"); got != "30d" {
			t.Errorf("range = %q, want 30d", got)
		}
		if got := r.URL.Query().Get("pageSize"); got != "100" {
			t.Errorf("pageSize = %q, want 100", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sess_history" {
			t.Errorf("authorization = %q", got)
		}
		page := r.URL.Query().Get("page")
		requestedPages = append(requestedPages, page)
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case "1":
			_, _ = w.Write([]byte(`{
				"code": 0,
				"data": {
					"data": {
						"items": [
							{
								"id": "request-1",
								"requestAt": "2026-09-19T15:59:59Z",
								"model": "claude-sonnet-4-5",
								"inputTokens": 10,
								"outputTokens": 2,
								"cacheReadTokens": 3,
								"cacheCreationTokens": 4,
								"prompt": "must not be retained",
								"preview": "must not be retained"
							},
							{
								"id": 2,
								"requestAt": "2026-09-19T16:00:00Z",
								"modelId": "gpt-5",
								"inputTokens": "20",
								"outputTokens": 5,
								"cacheReadTokens": 6,
								"cacheCreationTokens": 7
							}
						],
						"total": 3,
						"page": 1,
						"pageSize": 100
					}
				}
			}`))
		case "2":
			_, _ = w.Write([]byte(`{
				"data": {
					"items": [
						{
							"id": 2,
							"requestAt": "2026-09-19T16:00:00Z",
							"modelId": "gpt-5",
							"inputTokens": 9999,
							"outputTokens": 9999,
							"cacheReadTokens": 9999,
							"cacheCreationTokens": 9999
						},
						{
							"id": "request-3",
							"requestAt": "2026-09-20T01:30:00+08:00",
							"model": "gpt-5",
							"inputTokens": 30,
							"outputTokens": 8,
							"cacheReadTokens": 9,
							"cacheCreationTokens": 10
						}
					],
					"total": 3,
					"page": 2,
					"pageSize": 100
				}
			}`))
		default:
			http.Error(w, fmt.Sprintf("unexpected page %q", page), http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client := &Client{
		http:          server.Client(),
		usagePanelURL: server.URL + "/api/usage/panel?range=today&page=1&pageSize=1",
	}
	history, err := client.FetchUsageHistory(context.Background(), Credential{SessionToken: "sess_history"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(requestedPages) != 2 || requestedPages[0] != "1" || requestedPages[1] != "2" {
		t.Fatalf("requested pages = %v, want [1 2]", requestedPages)
	}
	if len(history) != 2 {
		t.Fatalf("history = %+v, want two Beijing dates", history)
	}
	first := history[0]
	if first.Date != "2026-09-19" || first.Timezone != "Asia/Shanghai" || first.InputTokens != 10 || first.OutputTokens != 2 || first.CacheReadTokens != 3 || first.CacheWriteTokens != 4 || first.Calls != 1 {
		t.Fatalf("unexpected first day: %+v", first)
	}
	if len(first.Models) != 1 || first.Models[0].Model != "claude-sonnet-4-5" || first.Models[0].Calls != 1 {
		t.Fatalf("unexpected first-day models: %+v", first.Models)
	}
	second := history[1]
	if second.Date != "2026-09-20" || second.InputTokens != 50 || second.OutputTokens != 13 || second.CacheReadTokens != 15 || second.CacheWriteTokens != 17 || second.Calls != 2 {
		t.Fatalf("unexpected second day (duplicate id may have been counted): %+v", second)
	}
	if len(second.Models) != 1 {
		t.Fatalf("unexpected second-day models: %+v", second.Models)
	}
	model := second.Models[0]
	if model.Model != "gpt-5" || model.InputTokens != 50 || model.OutputTokens != 13 || model.CacheReadTokens != 15 || model.CacheWriteTokens != 17 || model.Calls != 2 {
		t.Fatalf("unexpected gpt-5 aggregate: %+v", model)
	}
}
