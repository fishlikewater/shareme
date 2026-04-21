package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"message-share/backend/internal/api"
	appruntime "message-share/backend/internal/app"
	"message-share/backend/internal/config"
)

func TestRunReturnsConfigError(t *testing.T) {
	expectedErr := errors.New("bad config")

	err := run(context.Background(), discardLogger(), func() (config.AppConfig, error) {
		return config.AppConfig{}, expectedErr
	}, func(config.AppConfig, *log.Logger, *api.EventBus) runtimeHost {
		t.Fatal("host factory should not be called")
		return nil
	}, func(config.AppConfig, *log.Logger, runtimeHost, *api.EventBus) localUIHost {
		t.Fatal("local ui factory should not be called")
		return nil
	})

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected config error, got %v", err)
	}
}

func TestRunReturnsStartError(t *testing.T) {
	expectedErr := errors.New("start failed")
	host := &stubRuntimeHost{
		startErr: expectedErr,
		errs:     make(chan error),
	}

	err := run(context.Background(), discardLogger(), func() (config.AppConfig, error) {
		return config.AppConfig{DeviceName: "agent"}, nil
	}, func(config.AppConfig, *log.Logger, *api.EventBus) runtimeHost {
		return host
	}, func(config.AppConfig, *log.Logger, runtimeHost, *api.EventBus) localUIHost {
		t.Fatal("local ui factory should not be called")
		return nil
	})

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected start error, got %v", err)
	}
	if host.closeCalls != 0 {
		t.Fatalf("expected host.Close not to be called on start failure, got %d", host.closeCalls)
	}
}

func TestRunClosesHostOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	host := &stubRuntimeHost{
		errs: make(chan error),
	}
	done := make(chan error, 1)

	go func() {
		done <- run(ctx, discardLogger(), func() (config.AppConfig, error) {
			return config.AppConfig{
				DeviceName:          "office-pc",
				DataDir:             "C:/message-share",
				AgentTCPPort:        19090,
				LocalHTTPPort:       52350,
				DiscoveryUDPPort:    19091,
				AcceleratedDataPort: 19092,
			}, nil
		}, func(config.AppConfig, *log.Logger, *api.EventBus) runtimeHost {
			return host
		}, func(config.AppConfig, *log.Logger, runtimeHost, *api.EventBus) localUIHost {
			return &stubLocalUIHost{url: "http://127.0.0.1:52350/"}
		})
	}()

	cancel()

	if err := <-done; err != nil {
		t.Fatalf("expected nil error on context cancel, got %v", err)
	}
	if host.closeCalls != 1 {
		t.Fatalf("expected host.Close to be called once, got %d", host.closeCalls)
	}
}

func TestRunReturnsAsyncErrorAndClosesHost(t *testing.T) {
	expectedErr := errors.New("async failed")
	host := &stubRuntimeHost{
		errs: make(chan error, 1),
	}
	host.errs <- expectedErr

	err := run(context.Background(), discardLogger(), func() (config.AppConfig, error) {
		return config.AppConfig{
			DeviceName:          "office-pc",
			DataDir:             "C:/message-share",
			AgentTCPPort:        19090,
			LocalHTTPPort:       52350,
			DiscoveryUDPPort:    19091,
			AcceleratedDataPort: 19092,
		}, nil
	}, func(config.AppConfig, *log.Logger, *api.EventBus) runtimeHost {
		return host
	}, func(config.AppConfig, *log.Logger, runtimeHost, *api.EventBus) localUIHost {
		return &stubLocalUIHost{url: "http://127.0.0.1:52350/"}
	})

	if !errors.Is(err, expectedErr) {
		t.Fatalf("expected async error, got %v", err)
	}
	if !strings.Contains(err.Error(), "runtime host async error") {
		t.Fatalf("expected async error context, got %v", err)
	}
	if host.closeCalls != 1 {
		t.Fatalf("expected host.Close to be called once, got %d", host.closeCalls)
	}
}

func TestRunLogsLocalhostWebUIAddress(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runtimeHostStub := &stubRuntimeHost{errs: make(chan error)}
	localUIHostStub := &stubLocalUIHost{url: "http://127.0.0.1:52350/"}
	var output bytes.Buffer
	logger := log.New(&output, "", 0)

	done := make(chan error, 1)
	go func() {
		done <- run(
			ctx,
			logger,
			func() (config.AppConfig, error) {
				return config.AppConfig{
					DeviceName:          "office-pc",
					DataDir:             "C:/message-share",
					AgentTCPPort:        19090,
					LocalHTTPPort:       52350,
					DiscoveryUDPPort:    19091,
					AcceleratedDataPort: 19092,
				}, nil
			},
			func(config.AppConfig, *log.Logger, *api.EventBus) runtimeHost { return runtimeHostStub },
			func(config.AppConfig, *log.Logger, runtimeHost, *api.EventBus) localUIHost { return localUIHostStub },
		)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if !strings.Contains(output.String(), "http://127.0.0.1:52350/") {
		t.Fatalf("expected localhost url in output, got %q", output.String())
	}
}

func TestHeadlessProcessSmoke(t *testing.T) {
	if os.Getenv("MESSAGE_SHARE_HEADLESS_HELPER") == "1" {
		main()
		return
	}

	dataDir := filepath.Join(t.TempDir(), "headless-runtime")
	agentPort := reserveTCPPort(t)
	localHTTPPort := reserveTCPPort(t)
	acceleratedPort := reserveTCPPort(t)
	discoveryPort := reserveUDPPort(t)

	cmd := exec.Command(os.Args[0], "-test.run=TestHeadlessProcessSmoke")
	cmd.Env = append(os.Environ(),
		"MESSAGE_SHARE_HEADLESS_HELPER=1",
		"MESSAGE_SHARE_DATA_DIR="+dataDir,
		"MESSAGE_SHARE_AGENT_TCP_PORT="+strconv.Itoa(agentPort),
		"MESSAGE_SHARE_LOCAL_HTTP_PORT="+strconv.Itoa(localHTTPPort),
		"MESSAGE_SHARE_ACCELERATED_DATA_PORT="+strconv.Itoa(acceleratedPort),
		"MESSAGE_SHARE_DISCOVERY_UDP_PORT="+strconv.Itoa(discoveryPort),
		"MESSAGE_SHARE_DISCOVERY_LISTEN_ADDR=127.0.0.1:"+strconv.Itoa(discoveryPort),
		"MESSAGE_SHARE_DISCOVERY_BROADCAST_ADDR=127.0.0.1:"+strconv.Itoa(discoveryPort),
	)

	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output

	if err := cmd.Start(); err != nil {
		t.Fatalf("start helper process: %v", err)
	}

	exitCh := make(chan error, 1)
	go func() {
		exitCh <- cmd.Wait()
	}()

	t.Cleanup(func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		select {
		case <-exitCh:
		case <-time.After(3 * time.Second):
		}
	})

	waitForPath(t, filepath.Join(dataDir, "config.json"), 8*time.Second, &output)
	waitForPath(t, filepath.Join(dataDir, "local-device.json"), 8*time.Second, &output)
	waitForPath(t, filepath.Join(dataDir, "message-share.db"), 8*time.Second, &output)
	waitForHTTPReady(t, "http://127.0.0.1:"+strconv.Itoa(localHTTPPort)+"/api/bootstrap", 8*time.Second, &output)

	assertProcessStillRunning(t, exitCh, &output)
	time.Sleep(300 * time.Millisecond)
	assertProcessStillRunning(t, exitCh, &output)
}

func discardLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

func waitForPath(t *testing.T, path string, timeout time.Duration, output *bytes.Buffer) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("path %s was not created in time\noutput:\n%s", path, output.String())
}

func waitForHTTPReady(t *testing.T, url string, timeout time.Duration, output *bytes.Buffer) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		response, err := http.Get(url)
		if err == nil {
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	t.Fatalf("url %s was not ready in time\noutput:\n%s", url, output.String())
}

func assertProcessStillRunning(t *testing.T, exitCh <-chan error, output *bytes.Buffer) {
	t.Helper()

	select {
	case err := <-exitCh:
		t.Fatalf("helper process exited early: %v\noutput:\n%s", err, output.String())
	default:
	}
}

func reserveTCPPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve tcp port: %v", err)
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("unexpected tcp addr type %T", listener.Addr())
	}
	return addr.Port
}

func reserveUDPPort(t *testing.T) int {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve udp port: %v", err)
	}
	defer conn.Close()

	addr, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok {
		t.Fatalf("unexpected udp addr type %T", conn.LocalAddr())
	}
	return addr.Port
}

type stubRuntimeHost struct {
	mu         sync.Mutex
	startErr   error
	errs       chan error
	closeCalls int
}

func (s *stubRuntimeHost) Start(context.Context) error {
	return s.startErr
}

func (s *stubRuntimeHost) Close(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closeCalls++
	return nil
}

func (s *stubRuntimeHost) Errors() <-chan error {
	return s.errs
}

func (s *stubRuntimeHost) RuntimeService() *appruntime.RuntimeService {
	return nil
}

type stubLocalUIHost struct {
	url string
}

func (s *stubLocalUIHost) Start(context.Context) error { return nil }

func (s *stubLocalUIHost) Close(context.Context) error { return nil }

func (s *stubLocalUIHost) URL() string { return s.url }
