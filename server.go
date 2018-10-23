package smtpsrv

import (
	"crypto/tls"
	"net"
	"time"
)

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
}

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
		*config = *srv.TLSConfig
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
	var tempDelay time.Duration
	for {
		rw, e := l.Accept()
		if e != nil {
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
		go c.Serve()
	}
}
