package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"message-share/backend/internal/config"
	runtimehost "message-share/backend/internal/runtime"
)

const shutdownTimeout = 5 * time.Second

type runtimeHost interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
	Errors() <-chan error
}

type configLoader func() (config.AppConfig, error)

type hostFactory func(cfg config.AppConfig, logger *log.Logger) runtimeHost

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger, config.LoadDefault, newRuntimeHost); err != nil {
		logger.Fatal(err)
	}
}

func run(
	ctx context.Context,
	logger *log.Logger,
	loadConfig configLoader,
	makeHost hostFactory,
) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load default config: %w", err)
	}

	host := makeHost(cfg, logger)
	if err := host.Start(ctx); err != nil {
		return fmt.Errorf("start runtime host: %w", err)
	}
	defer shutdownHost(host, logger)

	logger.Printf(
		"message-share-agent running: device=%s dataDir=%s tcp=%d discovery=%d accelerated=%d",
		cfg.DeviceName,
		cfg.DataDir,
		cfg.AgentTCPPort,
		cfg.DiscoveryUDPPort,
		cfg.AcceleratedDataPort,
	)

	select {
	case <-ctx.Done():
		return nil
	case err := <-host.Errors():
		if err == nil {
			return nil
		}
		return fmt.Errorf("runtime host async error: %w", err)
	}
}

func shutdownHost(host runtimeHost, logger *log.Logger) {
	if host == nil {
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := host.Close(shutdownCtx); err != nil && logger != nil {
		logger.Printf("shutdown runtime host: %v", err)
	}
}

func newRuntimeHost(cfg config.AppConfig, logger *log.Logger) runtimeHost {
	return runtimehost.NewHost(runtimehost.Options{
		Config: cfg,
		Logger: logger,
	})
}
