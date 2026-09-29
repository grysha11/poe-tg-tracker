package grpcclient_test

import (
	"context"
	"net"
	"sync/atomic"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	"github.com/grysha11/poe-tg-tracker/internal/grpcclient"
	pb "github.com/grysha11/poe-tg-tracker/internal/pb/exchangev1"
)

type flakyQuery struct {
	pb.UnimplementedExchangeQueryServiceServer
	ratesCalls, leaguesCalls atomic.Int32
}

func (f *flakyQuery) GetRates(context.Context, *pb.GetRatesRequest) (*pb.GetRatesResponse, error) {
	if f.ratesCalls.Add(1) < 3 {
		return nil, status.Error(codes.Unavailable, "warming up")
	}
	return &pb.GetRatesResponse{League: "ok"}, nil
}

func (f *flakyQuery) ListLeagues(context.Context, *pb.ListLeaguesRequest) (*pb.ListLeaguesResponse, error) {
	f.leaguesCalls.Add(1)
	return nil, status.Error(codes.Unavailable, "upstream down")
}

func dial(t *testing.T, srv pb.ExchangeQueryServiceServer) pb.ExchangeQueryServiceClient {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	s := grpc.NewServer()
	pb.RegisterExchangeQueryServiceServer(s, srv)
	go s.Serve(lis)
	t.Cleanup(s.Stop)

	conn, err := grpcclient.Dial("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return pb.NewExchangeQueryServiceClient(conn)
}

func TestDial_RetriesUnavailable(t *testing.T) {
	srv := &flakyQuery{}
	resp, err := dial(t, srv).GetRates(context.Background(), &pb.GetRatesRequest{})
	if err != nil {
		t.Fatalf("GetRates: %v", err)
	}
	if resp.GetLeague() != "ok" || srv.ratesCalls.Load() != 3 {
		t.Errorf("league = %q after %d calls, want ok after 3", resp.GetLeague(), srv.ratesCalls.Load())
	}
}

func TestDial_DoesNotRetryUpstreamBoundMethods(t *testing.T) {
	srv := &flakyQuery{}
	_, err := dial(t, srv).ListLeagues(context.Background(), &pb.ListLeaguesRequest{})
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("err = %v, want Unavailable", err)
	}
	if srv.leaguesCalls.Load() != 1 {
		t.Errorf("ListLeagues calls = %d, want 1 (no client retry)", srv.leaguesCalls.Load())
	}
}
