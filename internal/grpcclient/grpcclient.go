package grpcclient

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc/filters"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const serviceConfig = `{
  "methodConfig": [
    {
      "name": [
        {"service": "exchange.v1.ExchangeQueryService"},
        {"service": "exchange.v1.ExchangeAdminService"}
      ],
      "retryPolicy": {
        "maxAttempts": 4,
        "initialBackoff": "0.1s",
        "maxBackoff": "2s",
        "backoffMultiplier": 2,
        "retryableStatusCodes": ["UNAVAILABLE"]
      }
    },
    {
      "name": [
        {"service": "exchange.v1.ExchangeQueryService", "method": "ListLeagues"},
        {"service": "exchange.v1.ExchangeAdminService", "method": "SyncCurrenciesFromScout"}
      ]
    }
  ]
}`

func Dial(addr string, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	opts = append([]grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler(otelgrpc.WithFilter(filters.Not(filters.HealthCheck())))),
		grpc.WithDefaultServiceConfig(serviceConfig),
	}, opts...)
	return grpc.NewClient(addr, opts...)
}
