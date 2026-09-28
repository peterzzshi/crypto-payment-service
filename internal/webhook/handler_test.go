package webhook

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"crypto-payment-service/internal/domain"
)

type stubDepositIngestor struct {
	called bool
	err    error
}

func (s *stubDepositIngestor) Process(_ context.Context, _ []byte) (*domain.Deposit, error) {
	s.called = true
	if s.err != nil {
		return nil, s.err
	}
	return &domain.Deposit{ID: "dep-123", Status: domain.DepositStatusConfirming}, nil
}

func TestDepositHandlerOK(t *testing.T) {
	btcIngestor := &stubDepositIngestor{}
	handler := &DepositHandler{Adapters: map[string]DepositIngestor{"btc": btcIngestor}}

	request := httptest.NewRequest(http.MethodPost, "/webhooks/btc", strings.NewReader("{}"))
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", recorder.Code)
	}
	if !btcIngestor.called {
		t.Fatalf("ingestor not called")
	}
}

func TestDepositHandlerNotFound(t *testing.T) {
	handler := &DepositHandler{Adapters: map[string]DepositIngestor{"btc": nil}}
	request := httptest.NewRequest(http.MethodPost, "/webhooks/eth", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", recorder.Code)
	}
}

func TestDepositHandlerBadPayload(t *testing.T) {
	badIngestor := &stubDepositIngestor{err: domain.WebhookBadPayloadError{Err: errors.New("bad payload")}}
	handler := &DepositHandler{Adapters: map[string]DepositIngestor{"btc": badIngestor}}
	request := httptest.NewRequest(http.MethodPost, "/webhooks/btc", strings.NewReader("{"))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", recorder.Code)
	}
}

func TestDepositHandlerHMACUnauthorized(t *testing.T) {
	handler := &DepositHandler{Adapters: map[string]DepositIngestor{"btc": &stubDepositIngestor{}}, Secret: "secret"}
	request := httptest.NewRequest(http.MethodPost, "/webhooks/btc", strings.NewReader("{}"))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", recorder.Code)
	}
}
