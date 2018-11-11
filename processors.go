package smtpsrv

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"net"
	"net/textproto"
	"strings"

	"github.com/smancke/mailck"
	"github.com/zaccone/spf"
)

func ehloProcessor(req *Request) error {
	if len(req.Line) < 2 {
		return req.TextProto.PrintfLine("%d %s", 501, "Not enough arguments")
	}

	req.Reset()
	req.HelloHost = req.Line[1]
	req.HelloRecieved = true

	err := req.TextProto.PrintfLine("%d-%s %s", 250, "Greets", req.Line[1])
	if err != nil {
		return err
	}
	if req.Server.TLSConfig != nil && req.TLSState == nil {
		err = req.TextProto.PrintfLine("%d-%s", 250, "STARTTLS")
		if err != nil {
			return err
		}
	}
	if ((req.Server.TLSConfig != nil && req.TLSState != nil) ||
		req.Server.TLSConfig == nil) &&
		req.Server.Auth != nil {
		err = req.TextProto.PrintfLine("%d-%s", 250, "AUTH PLAIN")
		if err != nil {
			return err
		}
	}
	err = req.TextProto.PrintfLine("%d-%s", 250, "PIPELINING")
	if err != nil {
		return err
	}
	err = req.TextProto.PrintfLine("%d-%s", 250, "SMTPUTF8")
	if err != nil {
		return err
	}
	return req.TextProto.PrintfLine("%d %s", 250, "8BITMIME")
}

func starttlsProcessor(req *Request) error {
	if req.Server.TLSConfig == nil {
		return req.TextProto.PrintfLine("%d %s", 454, "TLS unavailable on the server")
	}
	if req.TLSState != nil {
		return req.TextProto.PrintfLine("%d %s", 454, "TLS session already active")
	}

	err := req.TextProto.PrintfLine("%d %s", 220, "Ready to start TLS")
	if err != nil {
		return err
	}

	tlsconn := tls.Server(req.Conn, req.Server.TLSConfig)
	err = tlsconn.Handshake()
	if err != nil {
		return err
	}

	req.Conn = tlsconn
	req.TextProto = textproto.NewConn(req.Conn)
	req.TLSState = new(tls.ConnectionState)
	*req.TLSState = tlsconn.ConnectionState()
	req.HelloRecieved = false

	req.Reset()

	return nil
}

func authProcessor(req *Request) error {
	if req.Server.Auth == nil {
		return req.TextProto.PrintfLine("%d %s", 502, "Command not implemented")
	}
	if len(req.Line) < 2 {
		return req.TextProto.PrintfLine("%d %s", 501, "Not enough arguments")
	}
	ppwd := ""
	if len(req.Line) == 2 && req.Line[1] == "PLAIN" {
		err := req.TextProto.PrintfLine("%d %s", 334, "")
		if err != nil {
			return err
		}
		ppwd, err = req.TextProto.ReadLine()
		if err != nil {
			return err
		}
	}
	if len(req.Line) == 3 && req.Line[1] == "PLAIN" {
		ppwd = req.Line[2]
	}
	b, err := base64.StdEncoding.DecodeString(ppwd)
	if err != nil {
		return req.TextProto.PrintfLine("%d %s", 501, "Bad base64 encoding")
	}
	pparts := bytes.Split(b, []byte{0})
	if len(pparts) != 3 {
		return req.TextProto.PrintfLine("%d %s", 501, "Bad base64 encoding")
	}
	if err = req.Server.Auth(string(pparts[1]), string(pparts[2]), req.RemoteAddr); err == nil {
		req.AuthUser = string(pparts[1])
		return req.TextProto.PrintfLine("%d %s", 235, "2.7.0 Authentication successful")
	}
	req.AuthUser = ""
	return req.TextProto.PrintfLine("%d %s", 535, "5.7.8  Authentication credentials invalid")
}

func mailProcessor(req *Request) error {
	if req.Server.Auth != nil && req.AuthUser == "" {
		return req.TextProto.PrintfLine("%d %s", 503, "Authentication needed")
	}
	if req.From != "" {
		return req.TextProto.PrintfLine("%d %s", 503, "MAIL command already recieved")
	}
	if len(req.Line) < 2 {
		return req.TextProto.PrintfLine("%d %s", 501, "Not enough arguments")
	}
	if !strings.HasPrefix(req.Line[1], "FROM:") {
		return req.TextProto.PrintfLine("%d %s", 501, "MAIL command must be immediately succeeded by 'FROM:'")
	}
	i := strings.Index(req.Line[1], ":")
	if i < 0 || !emailRegExp.MatchString(req.Line[1][i+1:]) {
		return req.TextProto.PrintfLine("%d %s", 501, "MAIL command contained invalid address")
	}

	from := emailRegExp.FindStringSubmatch(req.Line[1][i+1:])[1]
	req.From = from
	ip, _, _ := net.SplitHostPort(req.RemoteAddr)

	// check the spf result
	req.SPFResult, _, _ = spf.CheckHost(net.ParseIP(ip), strings.Split(from, "@")[0], from)

	// check the format, host and user
	chkres, err := mailck.Check(from, from)
	req.MailValidation = (err == nil) && chkres.IsValid()

	return req.TextProto.PrintfLine("%d %s", 250, "Ok")
}

func rcptProcessor(req *Request) error {
	if req.From == "" {
		return req.TextProto.PrintfLine("%d %s", 503, "Bad sequence of commands")
	}
	if len(req.Line) < 2 {
		return req.TextProto.PrintfLine("%d %s", 501, "Not enough arguments")
	}
	if !strings.HasPrefix(req.Line[1], "TO:") {
		return req.TextProto.PrintfLine("%d %s", 501, "RCPT command must be immediately succeeded by 'TO:'")
	}
	i := strings.Index(req.Line[1], ":")
	if i < 0 || !emailRegExp.MatchString(req.Line[1][i+1:]) {
		return req.TextProto.PrintfLine("%d %s", 501, "RCPT command contained invalid address")
	}
	to := emailRegExp.FindStringSubmatch(req.Line[1][i+1:])[1]

	if req.Server.Addressable != nil && !req.Server.Addressable(req.AuthUser, to) {
		return req.TextProto.PrintfLine("%d %s", 501, "no such user - "+to)
	}

	req.To = append(req.To, to)
	return req.TextProto.PrintfLine("%d %s", 250, "Ok")
}

func dataProcessor(req *Request) error {
	if req.To == nil || req.From == "" || len(req.To) == 0 {
		return req.TextProto.PrintfLine("%d %s", 503, "Bad sequence of commands")
	}
	err := req.TextProto.PrintfLine("%d %s", 354, "End data with <CR><LF>.<CR><LF>")
	if err != nil {
		return err
	}

	req.Message = LimitDataSize(req.TextProto.DotReader(), req.Server.MaxBodySize)

	err = req.Server.Handler(req)
	if err != nil {
		req.Reset()
		return req.TextProto.PrintfLine("%d %s", 450, err.Error())
	}

	req.Reset()
	return req.TextProto.PrintfLine("%d %s", 250, "OK")
}

func rsetProcessor(req *Request) error {
	req.Reset()
	return req.TextProto.PrintfLine("%d %s", 250, "Ok")
}

func vrfyProcessor(req *Request) error {
	return req.TextProto.PrintfLine("%d %s", 250, "OK")
}

func expnProcessor(req *Request) error {
	return req.TextProto.PrintfLine("%d %s", 250, "OK")
}

func helpProcessor(req *Request) error {
	return req.TextProto.PrintfLine("%d %s", 250, "OK")
}

func noopProcessor(req *Request) error {
	return req.TextProto.PrintfLine("%d %s", 250, "OK")
}

func quitProcessor(req *Request) error {
	req.QuitSent = true
	return req.TextProto.PrintfLine("%d %s", 221, "OK")
}
