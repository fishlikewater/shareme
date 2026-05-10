package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"shareme/backend/internal/api"
	appruntime "shareme/backend/internal/app"
	"shareme/backend/internal/config"
	"shareme/backend/internal/frontendassets"
	"shareme/backend/internal/localui"
	runtimehost "shareme/backend/internal/runtime"
)

const shutdownTimeout = 5 * time.Second

type runtimeHost interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
	Errors() <-chan error
	RuntimeService() *appruntime.RuntimeService
}

type configLoader func() (config.AppConfig, error)

type hostFactory func(cfg config.AppConfig, logger *log.Logger, events *api.EventBus) runtimeHost
type localUIHost interface {
	Start(ctx context.Context) error
	Close(ctx context.Context) error
	URL() string
}

type localUIFactory func(cfg config.AppConfig, logger *log.Logger, runtime runtimeHost, events *api.EventBus) localUIHost

func main() {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger, config.LoadDefault, newRuntimeHost, newLocalUIHost); err != nil {
		logger.Fatal(err)
	}
}

func run(
	ctx context.Context,
	logger *log.Logger,
	loadConfig configLoader,
	makeRuntimeHost hostFactory,
	makeLocalUIHost localUIFactory,
) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load default config: %w", err)
	}

	events := api.NewEventBus()
	host := makeRuntimeHost(cfg, logger, events)
	if err := host.Start(ctx); err != nil {
		return fmt.Errorf("start runtime host: %w", err)
	}
	defer shutdownHost(host, logger)

	localUI := makeLocalUIHost(cfg, logger, host, events)
	if err := localUI.Start(ctx); err != nil {
		return fmt.Errorf("start localhost web ui: %w", err)
	}
	defer shutdownLocalUI(localUI, logger)

	logger.Printf(
		"shareme-agent running: device=%s dataDir=%s tcp=%d discovery=%d accelerated=%d web=%s",
		cfg.DeviceName,
		cfg.DataDir,
		cfg.AgentTCPPort,
		cfg.DiscoveryUDPPort,
		cfg.AcceleratedDataPort,
		localUI.URL(),
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

func shutdownLocalUI(host localUIHost, logger *log.Logger) {
	if host == nil {
		return
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := host.Close(shutdownCtx); err != nil && logger != nil {
		logger.Printf("shutdown localhost web ui: %v", err)
	}
}

func newRuntimeHost(cfg config.AppConfig, logger *log.Logger, events *api.EventBus) runtimeHost {
	return runtimehost.NewHost(runtimehost.Options{
		Config: cfg,
		Logger: logger,
		Events: appruntime.EventPublisherFunc(func(kind string, payload any) {
			if events != nil {
				events.Publish(kind, payload)
			}
		}),
	})
}

func newLocalUIHost(cfg config.AppConfig, logger *log.Logger, runtime runtimeHost, events *api.EventBus) localUIHost {
	assets, err := frontendassets.Select(embeddedFrontendAssets)
	if err != nil {
		return failedLocalUIHost{err: err}
	}

	return localui.NewHost(localui.HostOptions{
		Config: cfg,
		Logger: logger,
		Assets: assets,
		ResolveRuntime: func() localui.RuntimeCommands {
			if runtime == nil {
				return nil
			}
			return runtime.RuntimeService()
		},
		Bus: events,
	})
}

type failedLocalUIHost struct {
	err error
}

func (h failedLocalUIHost) Start(context.Context) error { return h.err }

func (failedLocalUIHost) Close(context.Context) error { return nil }

func (failedLocalUIHost) URL() string { return "" }
