package transfer

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

var ErrAcceleratedUnauthorized = errors.New("accelerated session unauthorized")

type AcceleratedSessionRegistration struct {
	SessionID     string
	TransferToken string
	ExpiresAt     time.Time
	Receiver      *AcceleratedReceiver
}

type acceleratedSessionBinding struct {
	transferToken string
	expiresAt     time.Time
	receiver      *AcceleratedReceiver
}

type AcceleratedListener struct {
	listener net.Listener

	mu       sync.RWMutex
	sessions map[string]acceleratedSessionBinding
	wg       sync.WaitGroup
}

func NewAcceleratedListener(listener net.Listener) *AcceleratedListener {
	return &AcceleratedListener{
		listener: listener,
		sessions: make(map[string]acceleratedSessionBinding),
	}
}

func (l *AcceleratedListener) Register(registration AcceleratedSessionRegistration) {
	if registration.SessionID == "" || registration.TransferToken == "" || registration.Receiver == nil {
		return
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.sessions[registration.SessionID] = acceleratedSessionBinding{
		transferToken: registration.TransferToken,
		expiresAt:     registration.ExpiresAt,
		receiver:      registration.Receiver,
	}
}

func (l *AcceleratedListener) Serve(ctx context.Context) error {
	stop := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = l.listener.Close()
		case <-stop:
		}
	}()
	defer close(stop)

	for {
		conn, err := l.listener.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				l.wg.Wait()
				return nil
			}
			return err
		}

		l.wg.Add(1)
		go func() {
			defer l.wg.Done()
			_ = l.handleConnection(ctx, conn)
		}()
	}
}

func (l *AcceleratedListener) Close() error {
	err := l.listener.Close()
	l.wg.Wait()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

func (l *AcceleratedListener) handleConnection(ctx context.Context, conn net.Conn) error {
	defer conn.Close()

	hello, err := ReadAcceleratedHello(conn)
	if err != nil {
		return err
	}

	binding, ok := l.lookup(hello.SessionID)
	if !ok {
		return ErrAcceleratedUnauthorized
	}
	if hello.TransferToken != binding.transferToken {
		return ErrAcceleratedUnauthorized
	}
	if !binding.expiresAt.IsZero() && time.Now().After(binding.expiresAt) {
		return ErrAcceleratedUnauthorized
	}

	return binding.receiver.ServeLane(ctx, hello.LaneIndex, conn)
}

func (l *AcceleratedListener) lookup(sessionID string) (acceleratedSessionBinding, bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	binding, ok := l.sessions[sessionID]
	return binding, ok
}
