package mailbox

import (
	"errors"
	"io"
	"strings"

	"github.com/tdewolff/parse/buffer"
)

// LimitDataSize Limit the incoming data size of the DATA command
func LimitDataSize(r io.Reader, s int64) io.Reader {
	if s > 0 {
		r = io.MultiReader(
			io.LimitReader(r, s),
			buffer.NewReader([]byte("\r\n.\r\n")),
		)
	}

	return r
}

// SplitAddress split the email@addre.ss to <user>@<domain>
func SplitAddress(address string) (string, string, error) {
	sepInd := strings.LastIndex(address, "@")
	if sepInd == -1 {
		return "", "", errors.New("Invalid Address:" + address)
	}
	localPart := address[:sepInd]
	domainPart := address[sepInd+1:]
	return localPart, domainPart, nil
}

// CanonicalizeEmail format the specified email address
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

// ListenAndServe start listening on the specified addr using the specified handler
func ListenAndServe(addr string, handler Handler) error {
	server := &Server{
		Addr:       addr,
		Handler:    handler,
		Processors: DefaultProcessors,
	}
	return server.ListenAndServe()
}

// ListenAndServeTLS start listening on the specified addr using the specified handler and tls configs
func ListenAndServeTLS(addr, certFile, keyFile string, handler Handler) error {
	server := &Server{Addr: addr, Handler: handler}
	return server.ListenAndServeTLS(certFile, keyFile)
}
