package smtpsrv

import (
	"errors"
	"regexp"
)

var (
	// ErrorRequestedActionAbortedLocalError ..
	ErrorRequestedActionAbortedLocalError = errors.New("Requested action aborted: local error in processing")
	// ErrorTransactionFailed ..
	ErrorTransactionFailed = errors.New("Transaction failed")
	// ErrorServiceNotAvailable ..
	ErrorServiceNotAvailable = errors.New("Service not available, closing transmission channel")
	// ErrorRequestedActionAbortedExceededStorage ..
	ErrorRequestedActionAbortedExceededStorage = errors.New("Requested mail action aborted: exceeded storage allocation")
)

var (
	// DefaultProcessors holds processor functions
	DefaultProcessors = map[string]Processor{
		"EHLO":     ehloProcessor,
		"HELO":     ehloProcessor,
		"STARTTLS": starttlsProcessor,
		"AUTH":     authProcessor,
		"MAIL":     mailProcessor,
		"RCPT":     rcptProcessor,
		"DATA":     dataProcessor,
		"RSET":     rsetProcessor,
		"VRFY":     vrfyProcessor,
		"EXPN":     expnProcessor,
		"HELP":     helpProcessor,
		"NOOP":     noopProcessor,
		"QUIT":     quitProcessor,
	}
)

var (
	emailRegExp = regexp.MustCompile(`^<((\S+)@(\S+))?>$`)
)
