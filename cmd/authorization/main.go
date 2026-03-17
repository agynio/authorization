package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	authorizationv1 "github.com/agynio/authorization/.gen/go/agynio/api/authorization/v1"
	"google.golang.org/grpc"

	"github.com/agynio/authorization/internal/config"
	"github.com/agynio/authorization/internal/server"
	openfgaclient "github.com/openfga/go-sdk/client"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("authorization: %v", err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := config.FromEnv()
	if err != nil {
		return err
	}

	fgaClient, err := openfgaclient.NewSdkClient(&openfgaclient.ClientConfiguration{
		ApiUrl: cfg.OpenFGAAPIURL,
	})
	if err != nil {
		return fmt.Errorf("create OpenFGA client: %w", err)
	}

	grpcServer := grpc.NewServer()
	authorizationv1.RegisterAuthorizationServiceServer(
		grpcServer,
		server.New(fgaClient, cfg.OpenFGAStoreID, cfg.OpenFGAModelID),
	)

	lis, err := net.Listen("tcp", cfg.GRPCAddress)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", cfg.GRPCAddress, err)
	}

	go func() {
		<-ctx.Done()
		grpcServer.GracefulStop()
	}()

	log.Printf("AuthorizationService listening on %s", cfg.GRPCAddress)

	if err := grpcServer.Serve(lis); err != nil {
		if errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
