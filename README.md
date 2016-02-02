A Go SMTP Server
===

Introduction
---
I wanted a way to bring email as a first class citezen in go. I modeled the api much like the http 
package. There were a few translation errors that I'm still trying to work out, things that are 
harder due to the fact that smtp is a stateful protocol. I'd welcome feedback on this.

A Very simple server
---

The smtp package works by offering a muxer that will route incoming email to registered handlers.
A simple server would register an 'all' handler, and then listen on the smtp port.

  import "github.com/murphysean/smtp"
  
  smtp.HandleFunc("*@*", func(envelope *smtp.Envelope) error {
		fmt.Println("Message Recieved", envelope.MessageTo)
		fmt.Println("From:", envelope.MessageFrom, envelope.RemoteAddr)
		fmt.Println("To:", envelope.MessageTo)
		fn := "emails/" + time.Now().Format(time.RFC3339) + ".eml"
		ioutil.WriteFile(fn, b, os.ModePerm)
		fmt.Println("Wrote to " + fn)
		return nil
  }
  
  log.Fatal(smtp.ListenAndServe(":smtp", nil))
  
Now all incoming messages will be logged and saved to the emails directory.

Additional Options
---

#### Security

The smtp server also supports the STARTTLS option, if you use the `ListenAndServeTLS` variant. You can
also further customize the tls config as well.

  server := smtp.Server{Name: "example.com", Debug: true}
  config := &tls.Config{MinVersion:tls.VersionSSL30}
  server.TLSConfig = config
  log.Fatal(server.ListenAndServeTLS(":smtp", "cert.pem", "key.pem", nil))
  
#### Naming and Debuging

As shown in the previous snippet you can also give your server a name (default = localhost). Naming lends
credibility to your server, that some clients seem to require.

Debugging is pretty verbose and dumps the entire protocol out to stderr. It is really handy for troubleshooting
particularly annoying clients.

#### Authentication

The smtp server also supports authentication via the PLAIN method. Ideally this would be coupled
with STARTTLS to ensure secrecy of passwords in transit. You can do this by creating a custom
server and registering the AUTH callback. This will be called everytime someone attempts to authenticate.

  server.Auth = func(username, password, remoteAddress string) error {
		if username == "user" && password == "p@$$w0rd" {
			return nil
		}
		return errors.New("Nope!")
	}

#### Addressing and preventing open-relay

Since your callback is only called once the smtp protocol has progressed to the data point, meaning
the sender and recipient have been specified, the server also offers a Addressable callback that
can be used to deny unknown recipients.

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
