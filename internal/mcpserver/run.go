package mcpserver

import (
	"context"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/bitbrew-dev/jevwise/internal/debuglog"
	"github.com/bitbrew-dev/jevwise/internal/service"
)

// RunOptions configures a foreground server. Ready receives only its public URL,
// never the bearer token. It must honor its supplied context; failure aborts
// startup and closes the listener.
type RunOptions struct {
	Address, Token string
	Ready          func(context.Context, string) error
	// Management must independently authenticate its private control credential.
	// The MCP agent bearer must not authorize management operations.
	Management http.Handler
}

// Run binds an exclusive literal-loopback listener and serves until cancellation.
// The caller owns the context-cooperative decision service. Timeout bounds
// decisions, not server life.
func Run(ctx context.Context, svc service.DecisionService, timeout time.Duration, options RunOptions) error {
	if ctx == nil {
		return errors.New("MCP server context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	address, err := ValidateAddress(options.Address)
	if err != nil {
		return err
	}
	var lc net.ListenConfig
	listener, err := lc.Listen(ctx, "tcp", address)
	if err != nil {
		return &toolError{"cannot bind MCP listener", err}
	}
	return Serve(ctx, listener, svc, timeout, options)
}

// Serve owns a prepared listener, including closing it on startup failure.
// Its actual bound address is authoritative; options.Address is only for Run.
// A prepared listener supports private daemon startup leases and isolated tests.
// Normal lifetime cancellation is a successful, bounded graceful shutdown.
func Serve(ctx context.Context, listener net.Listener, svc service.DecisionService, timeout time.Duration, options RunOptions) (resultErr error) {
	finish := debuglog.Trace(ctx, "mcp.serve")
	defer func() { finish(resultErr) }()
	if listener == nil {
		return errors.New("MCP listener is required")
	}
	defer listener.Close()
	if ctx == nil {
		return errors.New("MCP server context is required")
	}
	lifetime, cancel := context.WithCancel(ctx)
	defer cancel()
	server, err := New(lifetime, svc, timeout)
	if err != nil {
		return err
	}
	// Leave room for encoding and flushing the final response after tool timeout.
	if timeout > time.Duration(1<<63-1)-30*time.Second {
		return errors.New("MCP decision timeout is too large")
	}
	handler, err := NewHTTP(server, listener.Addr().String(), options.Token)
	if err != nil {
		return err
	}
	if options.Management != nil {
		mcpHandler := handler
		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.EscapedPath() {
			case "/_jev/status", "/_jev/stop":
				options.Management.ServeHTTP(w, r)
			default:
				mcpHandler.ServeHTTP(w, r)
			}
		})
	}
	httpServer := &http.Server{
		Handler: handler, MaxHeaderBytes: HTTPHeaderLimit,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second,
		WriteTimeout: timeout + 30*time.Second, IdleTimeout: 30 * time.Second,
		BaseContext: func(net.Listener) context.Context { return lifetime },
		// Standard HTTP diagnostics can contain peer data and arbitrary error text.
		ErrorLog: log.New(io.Discard, "", 0),
	}
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(listener) }()
	if err := lifetime.Err(); err != nil {
		_ = httpServer.Close()
		<-served
		return err
	}
	debuglog.Event(lifetime, "mcp.ready")
	if options.Ready != nil {
		if err := options.Ready(lifetime, "http://"+listener.Addr().String()+"/mcp"); err != nil {
			cancel()
			_ = httpServer.Close()
			<-served
			return &toolError{"cannot report MCP readiness", err}
		}
	}
	select {
	case err := <-served:
		cancel()
		_ = httpServer.Close()
		return &toolError{"MCP HTTP server stopped unexpectedly", err}
	case <-lifetime.Done():
	}
	// Cancellation reaches active decisions before the bounded connection drain.
	cancel()
	debuglog.Event(lifetime, "mcp.shutdown")
	shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	err = httpServer.Shutdown(shutdown)
	if err != nil {
		_ = httpServer.Close()
	}
	<-served
	if err != nil {
		return &toolError{"MCP server shutdown exceeded its limit", err}
	}
	return nil
}
