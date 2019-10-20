package smtpsrv

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"time"
)

// Handler is a 'net/http' like handler but for mails
type Handler func(req *Request) error

// Processor is a SMTP command processor
type Processor func(req *Request) error

// Server is our main server handler
type Server struct {
	// The name of the server used while greeting
	Name string

	// The address to listen on
	Addr string

	// The default inbox handler
	Handler Handler

	// If a tls config is set then this server will broadcast support
	// for the STARTTLS (RFC3207) extension.
	TLSConfig *tls.Config

	// Auth specifies an optional callback function that is called
	// when a client attempts to authenticate. If left nil (the default)
	// then the AUTH extension will not be supported.
	Auth func(username, password, remoteAddress string) error

	// Addressable specifies an optional callback function that is called
	// when a client attempts to send a message to the given address. This
	// allows the server to refuse messages that it doesn't own. If left nil
	// (the default) then the server will assume true
	Addressable func(user, address string) bool

	// Processors is a map of current supported commands' processor for incomming
	// SMTP messages/lines.
	Processors map[string]Processor

	// Maximum size of the DATA command in bytes
	MaxBodySize int64

	activeRequestsWG sync.WaitGroup
	serverClosed     bool
	listeners        []net.Listener
}

// ErrServerClosed is returned by Serve, ListenAndServe and ListenAndServeTLS
// after a call to Shutdown
var ErrServerClosed = errors.New("smtp: Server closed")

// ListenAndServe start serving the incoming data
func (srv *Server) ListenAndServe() error {
	if srv.Name == "" {
		srv.Name = "localhost"
	}
	addr := srv.Addr
	if addr == "" {
		addr = ":smtp"
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return srv.Serve(ln)
}

// ListenAndServeTLS start serving the incoming tls connection
func (srv *Server) ListenAndServeTLS(certFile string, keyFile string) error {
	config := &tls.Config{}
	if srv.TLSConfig != nil {
		config = srv.TLSConfig
	}
	var err error
	config.Certificates = make([]tls.Certificate, 1)
	config.Certificates[0], err = tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return err
	}
	srv.TLSConfig = config

	return srv.ListenAndServe()
}

// Serve start accepting the incoming connections
func (srv *Server) Serve(l net.Listener) error {
	defer l.Close()

	srv.listeners = append(srv.listeners, l)
	var tempDelay time.Duration
	for {
		rw, e := l.Accept()
		if e != nil {
			if srv.serverClosed {
				return ErrServerClosed
			}
			if ne, ok := e.(net.Error); ok && ne.Temporary() {
				if tempDelay == 0 {
					tempDelay = 5 * time.Millisecond
				} else {
					tempDelay *= 2
				}
				if max := 1 * time.Second; tempDelay > max {
					tempDelay = max
				}
				time.Sleep(tempDelay)
				continue
			}
			return e
		}
		tempDelay = 0
		c, err := NewRequest(rw, srv)
		if err != nil {
			continue
		}
		srv.activeRequestsWG.Add(1)
		go func() {
			c.Serve()
			srv.activeRequestsWG.Done()
		}()
	}
}

// Shutdown gracefully shutdowns the server.
// It first closes the listeners then waits for requests to finish. If the
// context expires before all requests have finished Shutdown will return the
// context's error, else it returns any error returned from closing the
// listeners.
func (srv *Server) Shutdown(ctx context.Context) error {
	srv.serverClosed = true

	var err error
	for _, l := range srv.listeners {
		lerr := l.Close()
		if lerr != nil {
			err = lerr
		}
	}

	waitChan := make(chan struct{})
	go func() {
		srv.activeRequestsWG.Wait()
		close(waitChan)
	}()

	select {
	case <-waitChan:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}
