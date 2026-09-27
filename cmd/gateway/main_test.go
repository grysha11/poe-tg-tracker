package main

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
)

type fakeQuery struct {
	pb.UnimplementedExchangeQueryServiceServer

	mu      sync.Mutex
	lastReq *pb.GetRatesRequest
}

func (f *fakeQuery) GetRates(_ context.Context, req *pb.GetRatesRequest) (*pb.GetRatesResponse, error) {
	f.mu.Lock()
	f.lastReq = req
	f.mu.Unlock()

	if req.GetLeague() == "nope" {
		return nil, status.Error(codes.NotFound, `no snapshots yet for league "nope"`)
	}
	return &pb.GetRatesResponse{
		Base:    &pb.CurrencyRef{TradeId: "divine", Name: "Divine Orb"},
		Rates:   []*pb.RankedRate{{Currency: &pb.CurrencyRef{TradeId: "mirror", Name: "Mirror of Kalandra"}, Vwap: 0.2}},
		HourUtc: 100,
		League:  req.GetLeague(),
	}, nil
}

func (f *fakeQuery) ListDefaultRatePairs(context.Context, *pb.ListDefaultRatePairsRequest) (*pb.ListDefaultRatePairsResponse, error) {
	return &pb.ListDefaultRatePairsResponse{Pairs: []*pb.DefaultRatePair{
		{Base: &pb.CurrencyRef{TradeId: "divine"}, Quote: &pb.CurrencyRef{TradeId: "chaos"}, SortOrder: 0},
	}}, nil
}

func (f *fakeQuery) last() *pb.GetRatesRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastReq
}

func newTestGateway(t *testing.T) (*httptest.Server, *fakeQuery, *health.Server) {
	t.Helper()
	fake := &fakeQuery{}
	hs := health.NewServer()

	lis := bufconn.Listen(1 << 20)
	srv := grpc.NewServer()
	pb.RegisterExchangeQueryServiceServer(srv, fake)
	healthpb.RegisterHealthServer(srv, hs)
	go srv.Serve(lis)
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial bufconn: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	h, err := newHandler(context.Background(), conn, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("newHandler: %v", err)
	}
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return ts, fake, hs
}

func get(t *testing.T, url string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	var body map[string]any
	json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode, body
}

func TestGetRates_TranslatesQueryParams(t *testing.T) {
	ts, fake, _ := newTestGateway(t)

	code, body := get(t, ts.URL+"/v1/rates?league=Forbidden%20Rites&view=RATE_VIEW_PRICE&limit=2")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %v)", code, body)
	}

	req := fake.last()
	if req.GetLeague() != "Forbidden Rites" || req.GetView() != pb.RateView_RATE_VIEW_PRICE || req.GetLimit() != 2 {
		t.Errorf("upstream got league=%q view=%v limit=%d, want Forbidden Rites/PRICE/2", req.GetLeague(), req.GetView(), req.GetLimit())
	}

	rates, _ := body["rates"].([]any)
	if len(rates) != 1 {
		t.Fatalf("rates = %v, want 1 entry", body["rates"])
	}
	cur := rates[0].(map[string]any)["currency"].(map[string]any)
	if cur["tradeId"] != "mirror" {
		t.Errorf("rates[0].currency.tradeId = %v, want mirror", cur["tradeId"])
	}
	// protojson encodes int64 as a JSON string; REST clients must parse it as such.
	if body["hourUtc"] != "100" {
		t.Errorf("hourUtc = %#v, want \"100\"", body["hourUtc"])
	}
}

func TestGetRates_NotFoundMapsTo404(t *testing.T) {
	ts, _, _ := newTestGateway(t)

	code, body := get(t, ts.URL+"/v1/rates?league=nope")
	if code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (body: %v)", code, body)
	}
	if body["message"] != `no snapshots yet for league "nope"` {
		t.Errorf("message = %v, want the upstream error message", body["message"])
	}
}

func TestListDefaultRatePairs_EmitsZeroValues(t *testing.T) {
	ts, _, _ := newTestGateway(t)

	code, body := get(t, ts.URL+"/v1/rate-pairs/default")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	pair := body["pairs"].([]any)[0].(map[string]any)
	if v, ok := pair["sortOrder"]; !ok || v != float64(0) {
		t.Errorf("sortOrder = %#v (present=%v), want explicit 0", v, ok)
	}
}

func TestHealthAndReadiness(t *testing.T) {
	ts, _, hs := newTestGateway(t)

	if code, _ := get(t, ts.URL+"/healthz"); code != http.StatusOK {
		t.Errorf("/healthz = %d, want 200", code)
	}
	if code, _ := get(t, ts.URL+"/readyz"); code != http.StatusOK {
		t.Errorf("/readyz with upstream SERVING = %d, want 200", code)
	}

	hs.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	if code, _ := get(t, ts.URL+"/readyz"); code != http.StatusServiceUnavailable {
		t.Errorf("/readyz with upstream NOT_SERVING = %d, want 503", code)
	}
}

func TestAdminIsNotExposed(t *testing.T) {
	ts, _, _ := newTestGateway(t)

	resp, err := http.Post(ts.URL+"/exchange.v1.ExchangeAdminService/CurateCurrency", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("admin path status = %d, want 404", resp.StatusCode)
	}
}
