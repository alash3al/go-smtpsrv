package smtpsrv

type HandlerFunc func(ctx *Context, apiKey string) error
type AuthFunc func(username, password string) error
