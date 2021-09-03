package service

import (
	"context"
	"encoding/json"
	"errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
	"net"
	"position-service/src/internal/data"
	"position-service/src/internal/repository"
	"position-service/src/protocol"
)

type server struct {
	database *repository.Postgres
	protocol.UnimplementedPositionServiceServer
}

func NewServer(postgres *repository.Postgres) *server {
	return &server{
		database:                           postgres,
		UnimplementedPositionServiceServer: protocol.UnimplementedPositionServiceServer{},
	}
}

func StartService(repository *repository.Postgres) {
	srv := NewServer(repository)
	go func() {
		err := srv.database.ListenNotify()
		log.Fatalf("fatal err: %v", err)
	}()

	go func() {
		err := srv.database.ListenPosPriceUpd("listen for pos upd")
		log.Fatalf("fatal err: %v", err)
	}()

	lis, err := net.Listen("tcp", "localhost:8082")
	if err != nil {
		log.Fatalf("Couldn't start listen: %v", err)
	}

	grpcServer := grpc.NewServer()
	protocol.RegisterPositionServiceServer(grpcServer, srv)
	if err = grpcServer.Serve(lis); err != nil {
		log.Fatalf("Failed to serve gRPC server over port 8082: %v", err)
	}
}

func (s server) Donate(ctx context.Context, req *protocol.DonateValue) (*protocol.Response, error) {
	if req.Val < 0 {
		return nil, errors.New("Invalid donate value ")
	}

	if err := s.database.IncreaseUserBalance(ctx, req.Token, req.Val); err != nil {
		return nil, err
	}

	return &protocol.Response{Message: "OK"}, nil
}

func (s server) GetUserBalance(ctx context.Context, req *protocol.Token) (*protocol.Balance, error) {
	if !s.database.ValidateToken(ctx, req.Token) {
		return nil, errors.New("Unauthorized ")
	}

	balance, err := s.database.GetUserBalance(ctx, req.Token)
	if err != nil {
		return nil, err
	}
	return &protocol.Balance{Balance: balance}, nil
}

func (s server) GetUserData(ctx context.Context, req *protocol.Token) (*protocol.UserData, error) {
	if !s.database.ValidateToken(ctx, req.Token) {
		return nil, errors.New("Unauthorized ")
	}
	positions, err := s.database.GetUserPositionsByToken(ctx, req.Token)
	if err != nil {
		return nil, err
	}

	balance, err := s.database.GetUserBalance(ctx, req.Token)
	if err != nil {
		return nil, err
	}

	positionsBytes, err := json.Marshal(positions)
	if err != nil {
		return nil, err
	}
	return &protocol.UserData{Positions: positionsBytes, Balance: balance}, nil
}

func (s server) Logout(ctx context.Context, token *protocol.Token) (*protocol.Response, error) {
	if err := s.database.Logout(ctx, token); err != nil {
		return nil, err
	}
	return &protocol.Response{Message: "OK"}, nil
}

func (s server) Login(ctx context.Context, req *protocol.LoginReq) (*protocol.LoginResp, error) {
	resp, err := s.database.Login(ctx, req)
	return resp, err
}

func (s server) SendClosePositionRequest(ctx context.Context, price *protocol.PositionCloseReq) (*protocol.Response, error) {
	if !s.database.ValidateToken(ctx, price.Token) {
		return &protocol.Response{Message: "FAIL"}, nil
	}

	if _, ok := s.database.PriceChannels[price.Symbol]; ok {
		if _, ok = s.database.PriceChannels[price.Symbol][price.Id]; ok {
			if err := s.database.ClosePosition(ctx, price); err == nil {
				return &protocol.Response{Message: "OK"}, nil
			}
		}
		return &protocol.Response{Message: "FAIL"}, nil
	}
	return &protocol.Response{Message: "FAIL"}, nil
}

func (s server) SendOpenPositionRequest(ctx context.Context, price *protocol.PositionOpenReq) (*protocol.Response, error) {
	receivedPrice := data.SymbolPrice{
		Uuid:   price.Uuid,
		Symbol: price.Symbol,
		Bid:    price.Bid,
		Ask:    price.Ask,
	}
	if s.database.LastPrices.Values[receivedPrice.Symbol] != receivedPrice {
		return &protocol.Response{Message: "FAIL"}, nil
	}

	if !s.database.ValidateToken(ctx, price.Token) {
		return &protocol.Response{Message: "FAIL"}, nil
	}

	if _, err := s.database.OpenPosition(ctx, price); err != nil {
		return &protocol.Response{Message: "FAIL"}, err
	}
	return &protocol.Response{Message: "OK"}, nil
}
