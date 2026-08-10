package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("path = %q, want /models", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test-123" {
			t.Errorf("Authorization = %q, want Bearer sk-test-123", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[
			{"id":"deepseek-chat","owned_by":"deepseek"},
			{"id":"deepseek-reasoner","name":"DeepSeek Reasoner","context_length":65536}
		]}`))
	}))
	defer srv.Close()

	models, err := FetchModels(context.Background(), srv.URL+"/", " sk-test-123 ")
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("models = %d, want 2", len(models))
	}
	if models[0].ID != "deepseek-chat" || models[0].Name != "deepseek-chat" || models[0].TokenLimit != 0 {
		t.Errorf("model[0] = %+v", models[0])
	}
	if models[1].ID != "deepseek-reasoner" || models[1].Name != "DeepSeek Reasoner" || models[1].TokenLimit != 65536 {
		t.Errorf("model[1] = %+v", models[1])
	}
}

func TestFetchModelsMaxModelLenFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen-max","max_model_len":131072}]}`))
	}))
	defer srv.Close()
	models, err := FetchModels(context.Background(), srv.URL, "k")
	if err != nil {
		t.Fatalf("FetchModels: %v", err)
	}
	if models[0].TokenLimit != 131072 {
		t.Errorf("TokenLimit = %d, want 131072", models[0].TokenLimit)
	}
}

func TestFetchModelsErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid key"}`))
	}))
	defer srv.Close()
	if _, err := FetchModels(context.Background(), srv.URL, "k"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("want 401 error, got %v", err)
	}
	if _, err := FetchModels(context.Background(), "", "k"); err == nil {
		t.Error("empty baseURL should error")
	}
	if _, err := FetchModels(context.Background(), "http://x", " "); err == nil {
		t.Error("empty key should error")
	}
}

func TestFetchModelsEmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()
	if _, err := FetchModels(context.Background(), srv.URL, "k"); err == nil {
		t.Error("empty model list should error")
	}
}
