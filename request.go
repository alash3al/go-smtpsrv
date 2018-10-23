package smtpsrv

import (
	"crypto/tls"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"sync"

	"github.com/zaccone/spf"
)

// Request is the incoming connection meta-data
type Request struct {
	Server    *Server
	Conn      net.Conn
	TextProto *textproto.Conn
	TLSState  *tls.ConnectionState

	RemoteAddr string

	HelloHost     string
	HelloRecieved bool

	AuthUser string
	From     string
	To       []string
	Message  *mail.Message

	QuitSent  bool
	SPFResult spf.Result

	Line []string

	mu sync.Mutex
}

// NewRequest creates a new instance of the Request struct
func NewRequest(conn net.Conn, srv *Server) (req *Request, err error) {
	req = new(Request)
	req.Reset()
	req.RemoteAddr = conn.RemoteAddr().String()
	req.Server = srv
	req.Conn = conn
	req.TextProto = textproto.NewConn(conn)
	req.TLSState = nil
	req.Line = []string{}
	return req, nil
}

// Serve start accepting incoming connections
func (req *Request) Serve() {
	err := req.TextProto.PrintfLine("%d %s %s", 220, req.Server.Name, "ESMTP")
	if err != nil {
		return
	}

	for !req.QuitSent && err == nil {
		err = req.Process()
		if err != nil {
			return
		}
	}

	req.TextProto.Close()
	req.Conn.Close()
}

// Reset resets to the defaults
func (req *Request) Reset() {
	req.From = ""
	req.To = make([]string, 0)
	req.Message = nil
}

// Process start parsing and processing the current command-line
func (req *Request) Process() error {
	s, err := req.TextProto.ReadLine()
	if err != nil {
		return err
	}

	req.Line = strings.Split(s, " ")
	if len(req.Line) <= 0 {
		return req.TextProto.PrintfLine("%d %s (%s)", 500, "Command not recognized", s)
	}

	if req.Server.Processors == nil {
		req.Server.Processors = DefaultProcessors
	}

	processor, found := req.Server.Processors[req.Line[0]]
	if !found {
		return req.TextProto.PrintfLine("%d %s (%s)", 500, "Command not recognized", req.Line[0])
	}

	return processor(req)
}
