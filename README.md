A SMTP Server Package [![](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=flat-square)](https://godoc.org/github.com/alash3al/go-smtpsrv)
=============================
a simple smtp server library, forked from [This repo](http://github.com/murphysean/smtp) but I refactored it to be more advanced and organized

Features
=========
- Very simple
- Automated SPF Checks
- Automated `FROM` validation (`MX` checking), via `Request.Mailable`
- Supports TLS
- Modular, as you can add more smtp command processors to extend the functionality as you need

Quick Start
===========
> `go get github.com/alash3al/go-smtpsrv`

```go
package main

import (
	"fmt"

	"github.com/alash3al/go-smtpsrv"
)

func main() {
	handler := func(req *smtpsrv.Request) error {
		// ...
		return nil
	}
	srv := &smtpsrv.Server{
		Addr:        ":25025",
		MaxBodySize: 5 * 1024,
		Handler:     handler,
	}
	fmt.Println(srv.ListenAndServe())
}

```

#### Security

The smtp server also supports the STARTTLS option, if you use the `ListenAndServeTLS` variant.
You can also further customize the tls config as well.

	server := smtp.Server{Name: "example.com", Debug: true}
	config := &tls.Config{MinVersion:tls.VersionSSL30}
	server.TLSConfig = config
	log.Fatal(server.ListenAndServeTLS(":smtp", "cert.pem", "key.pem", nil))

#### Authentication

The smtp server also supports authentication via the PLAIN method. Ideally this would be 
coupled with STARTTLS to ensure secrecy of passwords in transit. You can do this by creating 
a custom server and registering the AUTH callback. This will be called everytime someone 
attempts to authenticate.

	server.Auth = func(username, password, remoteAddress string) error {
		if username == "user" && password == "p@$$w0rd" {
			return nil
		}
		return errors.New("Nope!")
	}

#### Addressing and preventing open-relay

Since your callback is only called once the smtp protocol has progressed to the data point, 
meaning the sender and recipient have been specified, the server also offers an Addressable 
callback that can be used to deny unknown recipients.

	server.Addressable = func(user, address string) bool {
		if user != ""{
			//Allow relay for authenticated users
			return true
		}
		if strings.HasSuffix(address, "example.com"){
			return true
		}
		return false
	}
