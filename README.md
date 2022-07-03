A SMTP Server Package [![](https://img.shields.io/badge/godoc-reference-5272B4.svg?style=flat-square)](https://godoc.org/github.com/stouch/go-smtpsrv)
=============================
a simple smtp server library for writing email servers like a boss.

Quick Start
===========
> `go get github.com/stouch/go-smtpsrv`

```go
package main

import (
	"fmt"

	"github.com/stouch/go-smtpsrv/v3"
)

func main() {
	handler := func(c smtpsrv.Context) error {
		// ...
		return nil
	}

	cfg := smtpsrv.ServerConfig{
		BannerDomain:  "mail.my.server",
		ListenAddress: ":25025",
		MaxMessageBytes: 5 * 1024,
		Handler:     handler,
	}

	fmt.Println(smtpsrv.ListenAndServe(cfg))
}

```

Thanks
=======
- [parsemail](https://github.com/DusanKasan/parsemail)
- [go-smtp](github.com/emersion/go-smtp)
