package smtpsrv

// Handler is a 'net/http' like handler but for mails
type Handler func(req *Request) error

// Processor is a SMTP command processor
type Processor func(req *Request) error
