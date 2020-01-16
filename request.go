package smtpsrv

import (
	"crypto/tls"
	"net"
	"net/mail"
	"net/textproto"
	"strings"

	"github.com/zaccone/spf"
)

// Request is the incoming connection meta-data
type Request struct {
	// the currently running server
	Server *Server

	// the underlying socket for currently connected client
	Conn net.Conn

	// an instance of go stdlib for handling the above Conn as a text-porotocol
	TextProto *textproto.Conn

	// TLS related info
	TLSState *tls.ConnectionState

	// a shortcut for Conn.RemoteAddr()
	RemoteAddr string

	// contains the hostname of the currently connected client during the EHLO/HELO command
	HelloHost string

	// whether EHLO/HELO called or not
	HelloRecieved bool

	// whether MAIL FROM was received or not
	MailFromReceived bool

	// the login username used for login, empty means that this is an anonymous attempt
	AuthUser string

	// the user that sends the mail
	From string

	// the rctps!
	To []string

	// the body of the mail "DATA command" but parsed
	Message *mail.Message

	// whether the client called QUIT or not
	QuitSent bool

	// the spf checking result
	SPFResult spf.Result

	// whether the FROM mail is mailable or not
	Mailable bool

	// the currently processing line
	Line []string
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
	defer func() {
		req.TextProto.Close()
		req.Conn.Close()
	}()
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
}

// Reset resets to the defaults
func (req *Request) Reset() {
	req.From = ""
	req.MailFromReceived = false
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

	req.Line[0] = strings.ToUpper(req.Line[0])

	processor, found := req.Server.Processors[req.Line[0]]
	if !found {
		return req.TextProto.PrintfLine("%d %s (%s)", 500, "Command not recognized", req.Line[0])
	}

	return processor(req)
}
