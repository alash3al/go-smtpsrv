package smtp

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net"
	"net/textproto"
	"regexp"
	"strings"
	"sync"
	"time"
)

var emailRegExp = regexp.MustCompile(`^<((\S+)@(\S+\.\S+))>$`)

type Server struct {
	Name    string
	Addr    string
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

	Debug    bool
	ErrorLog *log.Logger
}

type conn struct {
	remoteAddr string
	server     *Server
	rwc        net.Conn
	text       *textproto.Conn
	tlsState   *tls.ConnectionState

	fromAgent     string
	user          string
	mailFrom      string
	mailTo        []string
	mailData      *bytes.Buffer
	helloRecieved bool
	quitSent      bool

	mu sync.Mutex
}

type HandlerFunc func(envelope *Envelope) error

func (f HandlerFunc) ServeSMTP(envelope *Envelope) error {
	return f(envelope)
}

type Envelope struct {
	FromAgent   string
	RemoteAddr  string
	User        string
	MessageFrom string
	MessageTo   string
	MessageData io.Reader
}

var (
	ErrorRequestedActionAbortedLocalError      = errors.New("Requested action aborted: local error in processing")
	ErrorTransactionFailed                     = errors.New("Transaction failed")
	ErrorServiceNotAvailable                   = errors.New("Service not available, closing transmission channel")
	ErrorRequestedActionAbortedExceededStorage = errors.New("Requested mail action aborted: exceeded storage allocation")
)

type Handler interface {
	ServeSMTP(envelope *Envelope) error
}

type ServeMux struct {
	mu sync.RWMutex
	m  map[string]map[string]muxEntry
}

type muxEntry struct {
	h       Handler
	pattern string
}

func (srv *Server) logfd(format string, args ...interface{}) {
	if srv.Debug {
		srv.logf(format, args...)
	}
}

func (srv *Server) logf(format string, args ...interface{}) {
	if srv.ErrorLog != nil {
		srv.ErrorLog.Printf(format, args...)
	} else {
		log.Printf(format, args...)
	}
}

func (srv *Server) newConn(rwc net.Conn) (c *conn, err error) {
	c = new(conn)
	c.resetSession()
	c.remoteAddr = rwc.RemoteAddr().String()
	c.server = srv
	c.rwc = rwc
	c.text = textproto.NewConn(c.rwc)
	c.tlsState = nil
	return c, nil
}

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
				srv.logf("smtp: Accept error: %v; retrying in %v", e, tempDelay)
				time.Sleep(tempDelay)
				continue
			}
			return e
		}
		tempDelay = 0
		c, err := srv.newConn(rw)
		if err != nil {
			continue
		}
		go c.serve()
	}
}

func (c *conn) serve() {
	c.server.logfd("INFO: Handling new connection from " + c.remoteAddr)
	c.server.logfd("<%d %s %s\n", 220, c.server.Name, "ESMTP")
	err := c.text.PrintfLine("%d %s %s", 220, c.server.Name, "ESMTP")
	if err != nil {
		c.server.logf("%v\n", err)
		return
	}
	for !c.quitSent && err == nil {
		err = c.readCommand()
		if err != nil {
			c.server.logf("%v\n", err)
		}
	}
	c.text.Close()
	c.rwc.Close()
}

func (c *conn) resetSession() {
	//TODO See if reseting will log out the currently authed user?
	//c.user = ""
	c.mailFrom = ""
	c.mailTo = make([]string, 0)
	c.mailData = nil
}

func (c *conn) readCommand() error {
	s, err := c.text.ReadLine()
	if err != nil {
		return err
	}
	c.server.logfd(">%s\n", s)
	parts := strings.Split(s, " ")
	if len(parts) <= 0 {
		c.server.logfd("<%d %s\n", 500, "Command not recognized")
		return c.text.PrintfLine("%d %s", 500, "Command not recognized")
	}
	switch parts[0] {
	case "HELO":
		if len(parts) < 2 {
			c.server.logfd("<%d %s\n", 501, "Not enough arguments")
			return c.text.PrintfLine("%d %s", 501, "Not enough arguments")
		}
		c.fromAgent = parts[1]
		c.resetSession()
		c.helloRecieved = true
		c.server.logfd("<%d %s %s\n", 250, "Hello", parts[1])
		return c.text.PrintfLine("%d %s %s", 250, "Hello", parts[1])
	case "EHLO":
		if len(parts) < 2 {
			c.server.logfd("<%d %s\n", 501, "Not enough arguments")
			return c.text.PrintfLine("%d %s", 501, "Not enough arguments")
		}
		c.fromAgent = parts[1]
		c.resetSession()
		c.helloRecieved = true
		c.server.logfd("<%d-%s %s\n", 250, "Greets", parts[1])
		err := c.text.PrintfLine("%d-%s %s", 250, "Greets", parts[1])
		if err != nil {
			return err
		}
		if c.server.TLSConfig != nil && c.tlsState == nil {
			c.server.logfd("<%d-%s\n", 250, "STARTTLS")
			err = c.text.PrintfLine("%d-%s", 250, "STARTTLS")
			if err != nil {
				return err
			}
		}
		if c.server.Auth != nil {
			c.server.logfd("<%d-%s\n", 250, "AUTH PLAIN")
			err = c.text.PrintfLine("%d-%s", 250, "AUTH PLAIN")
			if err != nil {
				return err
			}
		}
		c.server.logfd("<%d-%s\n", 250, "PIPELINING")
		err = c.text.PrintfLine("%d-%s", 250, "PIPELINING")
		if err != nil {
			return err
		}
		c.server.logfd("<%d-%s\n", 250, "SMTPUTF8")
		err = c.text.PrintfLine("%d-%s", 250, "SMTPUTF8")
		if err != nil {
			return err
		}
		c.server.logfd("<%d %s\n", 250, "8BITMIME")
		return c.text.PrintfLine("%d %s", 250, "8BITMIME")
	case "STARTTLS":
		//Check to see if the server supports
		if c.server.TLSConfig == nil {
			//Error out here
			c.server.logfd("<%d %s\n", 454, "TLS unavailable on the server")
			return c.text.PrintfLine("%d %s", 454, "TLS unavailable on the server")
		}
		if c.tlsState != nil {
			//Already in a tls state send error
			c.server.logfd("<%d %s\n", 454, "TLS session already active")
			return c.text.PrintfLine("%d %s", 454, "TLS session already active")
		}
		//If there is support, write out the resp
		c.server.logfd("<%d %s\n", 220, "Ready to start TLS")
		err = c.text.PrintfLine("%d %s", 220, "Ready to start TLS")
		if err != nil {
			return err
		}
		//Remap the underlying connection to a tls connection
		tlsconn := tls.Server(c.rwc, c.server.TLSConfig)
		err = tlsconn.Handshake()
		if err != nil {
			return err
		}
		c.rwc = tlsconn
		//Remap the underlying textproto connection to work on top of the tls conn
		c.text = textproto.NewConn(c.rwc)
		c.tlsState = new(tls.ConnectionState)
		*c.tlsState = tlsconn.ConnectionState()
		c.resetSession()
		c.helloRecieved = false
	case "AUTH":
		if c.server.Auth == nil {
			c.server.logfd("<%d %s\n", 502, "Command not implemented")
			return c.text.PrintfLine("%d %s", 502, "Command not implemented")
		}
		//TODO All Auth must occur over TLS
		//if c.tlsState == nil {
		//	c.server.logfd("<%d %s\n", 538, "5.7.11 Encryption required for requested authentication mechanism")
		//	return c.text.PrintfLine("%d %s", 538, "5.7.11 Encryption required for requested authentication mechanism")
		//}
		if len(parts) < 2 {
			c.server.logfd("<%d %s\n", 501, "Not enough arguments")
			return c.text.PrintfLine("%d %s", 501, "Not enough arguments")
		}
		ppwd := ""
		if len(parts) == 2 && parts[1] == "PLAIN" {
			c.server.logfd("<%d %s\n", 334, "")
			err := c.text.PrintfLine("%d %s", 334, "")
			if err != nil {
				return err
			}
			//Now read the plain password
			ppwd, err := c.text.ReadLine()
			if err != nil {
				return err
			}
			c.server.logfd(">%s\n", ppwd)
		}
		if len(parts) == 3 && parts[1] == "PLAIN" {
			ppwd = parts[2]
		}
		//Call the call back method with the username and password
		b, err := base64.StdEncoding.DecodeString(ppwd)
		if err != nil {
			c.server.logfd("<%d %s\n", 501, "Bad base64 encoding")
			return c.text.PrintfLine("%d %s", 501, "Bad base64 encoding")
		}
		pparts := bytes.Split(b, []byte{0})
		if len(pparts) != 3 {
			c.server.logfd("<%d %s\n", 501, "Bad base64 encoding")
			return c.text.PrintfLine("%d %s", 501, "Bad base64 encoding")
		}
		if err = c.server.Auth(string(pparts[1]), string(pparts[2]), c.remoteAddr); err == nil {
			c.user = string(pparts[1])
			c.server.logfd("<%d %s\n", 235, "2.7.0 Authentication successful")
			return c.text.PrintfLine("%d %s", 235, "2.7.0 Authentication successful")
		} else {
			c.user = ""
			c.server.logfd("<%d %s\n", 535, "5.7.8  Authentication credentials invalid")
			return c.text.PrintfLine("%d %s", 535, "5.7.8  Authentication credentials invalid")
		}
	case "MAIL":
		if c.mailFrom != "" {
			c.server.logfd("<%d %s\n", 503, "MAIL command already recieved")
			return c.text.PrintfLine("%d %s", 503, "MAIL command already recieved")
		}
		if len(parts) < 2 {
			c.server.logfd("<%d %s\n", 501, "Not enough arguments")
			return c.text.PrintfLine("%d %s", 501, "Not enough arguments")
		}
		if !strings.HasPrefix(parts[1], "FROM:") {
			c.server.logfd("<%d %s\n", 501, "MAIL command must be immediately succeeded by 'FROM:'")
			return c.text.PrintfLine("%d %s", 501, "MAIL command must be immediately succeeded by 'FROM:'")
		}
		i := strings.Index(parts[1], ":")
		if i < 0 || !emailRegExp.MatchString(parts[1][i+1:]) {
			c.server.logfd("<%d %s\n", 501, "MAIL command contained invalid address")
			return c.text.PrintfLine("%d %s", 501, "MAIL command contained invalid address")
		}
		from := emailRegExp.FindStringSubmatch(parts[1][i+1:])[1]
		c.mailFrom = from
		c.server.logfd("<%d %s\n", 250, "Ok")
		return c.text.PrintfLine("%d %s", 250, "Ok")
	case "RCPT":
		if c.mailFrom == "" {
			c.server.logfd("<%d %s\n", 503, "Bad sequence of commands")
			return c.text.PrintfLine("%d %s", 503, "Bad sequence of commands")
		}
		if len(parts) < 2 {
			c.server.logfd("<%d %s\n", 501, "Not enough arguments")
			return c.text.PrintfLine("%d %s", 501, "Not enough arguments")
		}
		if !strings.HasPrefix(parts[1], "TO:") {
			c.server.logfd("<%d %s\n", 501, "RCPT command must be immediately succeeded by 'TO:'")
			return c.text.PrintfLine("%d %s", 501, "RCPT command must be immediately succeeded by 'TO:'")
		}
		i := strings.Index(parts[1], ":")
		if i < 0 || !emailRegExp.MatchString(parts[1][i+1:]) {
			c.server.logfd("<%d %s\n", 501, "RCPT command contained invalid address")
			return c.text.PrintfLine("%d %s", 501, "RCPT command contained invalid address")
		}
		to := emailRegExp.FindStringSubmatch(parts[1][i+1:])[1]
		//Check the handler to see if the inbox has a registered listener
		if c.server.Addressable != nil && !c.server.Addressable(c.user, to) {
			c.server.logfd("<%d %s\n", 501, "no such user - "+to)
			return c.text.PrintfLine("%d %s", 501, "no such user - "+to)
		}
		c.mailTo = append(c.mailTo, to)
		c.server.logfd("<%d %s\n", 250, "Ok")
		return c.text.PrintfLine("%d %s", 250, "Ok")
	case "DATA":
		if c.mailTo == nil || c.mailFrom == "" || len(c.mailTo) == 0 {
			c.server.logfd("<%d %s\n", 503, "Bad sequence of commands")
			return c.text.PrintfLine("%d %s", 503, "Bad sequence of commands")
		}
		c.server.logfd("<%d %s\n", 354, "End data with <CR><LF>.<CR><LF>")
		err := c.text.PrintfLine("%d %s", 354, "End data with <CR><LF>.<CR><LF>")
		if err != nil {
			return err
		}
		b, err := c.text.ReadDotBytes()
		if err != nil {
			return err
		}
		c.server.logfd(">%s\n", b)
		c.mailData = bytes.NewBuffer(b)

		//Iterate over all of the to addresses, and include this message
		for _, v := range c.mailTo {
			err = c.server.Handler.ServeSMTP(&Envelope{c.fromAgent, c.remoteAddr, c.user, c.mailFrom, v, bytes.NewReader(c.mailData.Bytes())})
		}
		if err != nil {
			c.resetSession()
			c.server.logfd("<%d %s\n", 450, "Mailbox unavailable")
			return c.text.PrintfLine("%d %s", 450, "Mailbox unavailable")
		}

		//Reset the to,from, and data fields
		c.resetSession()

		c.server.logfd("<%d %s\n", 250, "OK")
		return c.text.PrintfLine("%d %s", 250, "OK")
	case "RSET":
		c.resetSession()
		c.server.logfd("<%d %s\n", 250, "Ok")
		return c.text.PrintfLine("%d %s", 250, "Ok")
	case "VRFY":
		//TODO By default this will check the handlers to see if the inbox is registered
		c.server.logfd("<%d %s\n", 250, "OK")
		return c.text.PrintfLine("%d %s", 250, "OK")
	case "EXPN":
		//TODO Call a callback to get the expanded list
		c.server.logfd("<%d %s\n", 250, "OK")
		return c.text.PrintfLine("%d %s", 250, "OK")
	case "HELP":
		c.server.logfd("<%d %s\n", 250, "OK")
		return c.text.PrintfLine("%d %s", 250, "OK")
	case "NOOP":
		c.server.logfd("<%d %s\n", 250, "OK")
		return c.text.PrintfLine("%d %s", 250, "OK")
	case "QUIT":
		c.server.logfd("<%d %s\n", 221, "OK")
		c.quitSent = true
		return c.text.PrintfLine("%d %s", 221, "OK")
	default:
		c.server.logfd("<%d %s\n", 500, "Command not recognized")
		return c.text.PrintfLine("%d %s", 500, "Command not recognized")
	}
	return nil
}

func NewServeMux() *ServeMux { return &ServeMux{m: make(map[string]map[string]muxEntry)} }

var DefaultServeMux = NewServeMux()

func SplitAddress(address string) (string, string, error) {
	sepInd := strings.LastIndex(address, "@")
	if sepInd == -1 {
		return "", "", errors.New("Invalid Address:" + address)
	}
	localPart := address[:sepInd]
	domainPart := address[sepInd+1:]
	return localPart, domainPart, nil
}

// Handle will register the given email pattern to be handled with the
// given handler. The Canonacalize method will be called on the pattern
// to help ensure expected matches will come through. The special '*'
// wildcard can be used for the local portion or the domain portion to
// broaden the match.
func (mux *ServeMux) Handle(pattern string, handler Handler) {
	mux.mu.Lock()
	l, d, err := SplitAddress(pattern)
	if err != nil {
		log.Fatal(err)
	}
	if l == "" {
		l = "*"
	}
	//TODO Should I Canonicalize here, or just warn if the canonical doesn't match the input?
	//Think scenario where sean+murphy+tag@blah.com
	cl := CanonicalizeEmail(l)
	dp, ok := mux.m[d]
	if !ok {
		dp = make(map[string]muxEntry)
		mux.m[d] = dp
	}
	ap, ok := dp[cl]
	if ok {
		log.Fatal("Handle Pattern already used!", pattern)
	}
	ap.h = handler
	ap.pattern = pattern
	dp[l] = ap
	defer mux.mu.Unlock()
}

func Handle(pattern string, handler Handler) {
	DefaultServeMux.Handle(pattern, handler)
}

func (mux *ServeMux) HandleFunc(pattern string, handler func(envelope *Envelope) error) {
	mux.Handle(pattern, HandlerFunc(handler))
}

func HandleFunc(pattern string, handler func(envelope *Envelope) error) {
	DefaultServeMux.HandleFunc(pattern, handler)
}

// I'm following googles example here. Basically all '.' dont matter in the local portion.
// Also you can append a '+' section to the end of your email and it will still route to you.
// This allows categorization of emails when they are given out. The muxer will canonicalize
// incoming email addresses to route them to a handler. The handler will still see the original
// email in the to portion.
func CanonicalizeEmail(local string) string {
	//Shouldn't be needed
	local = strings.TrimSpace(local)
	//To lower it all
	local = strings.ToLower(local)
	//Get rid of punctuation
	local = strings.Replace(local, ".", "", -1)
	//Get rid of anything after the last +
	if li := strings.LastIndex(local, "+"); li > 0 {
		local = local[:li]
	}
	return local
}

func (mux *ServeMux) ServeSMTP(envelope *Envelope) error {
	l, d, err := SplitAddress(envelope.MessageTo)
	cl := CanonicalizeEmail(l)
	if err != nil {
		return errors.New("Invalid Address")
	}
	if dp, ok := mux.m[d]; ok {
		if ap, ok := dp[cl]; ok {
			return ap.h.ServeSMTP(envelope)
		} else {
			if ap, ok := dp["*"]; ok {
				return ap.h.ServeSMTP(envelope)
			} else {
				return errors.New("Bad Address")
			}
		}
	} else {
		if dp, ok := mux.m["*"]; ok {
			if ap, ok := dp[cl]; ok {
				return ap.h.ServeSMTP(envelope)
			} else {
				if ap, ok := dp["*"]; ok {
					return ap.h.ServeSMTP(envelope)
				} else {
					return errors.New("Bad Address")
				}
			}
		} else {
			return errors.New("Bad Address")
		}
	}

	return nil
}

func ListenAndServe(addr string, handler Handler) error {
	if handler == nil {
		handler = DefaultServeMux
	}
	server := &Server{Addr: addr, Handler: handler}
	return server.ListenAndServe()
}

func ListenAndServeTLS(addr, certFile, keyFile string, handler Handler) error {
	if handler == nil {
		handler = DefaultServeMux
	}
	server := &Server{Addr: addr, Handler: handler}
	return server.ListenAndServeTLS(certFile, keyFile)
}
