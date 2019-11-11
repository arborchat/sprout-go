package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	sprout "git.sr.ht/~whereswaldon/sprout-go"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	certpath := flag.String("certpath", "", "Location of the TLS public key (certificate file)")
	keypath := flag.String("keypath", "", "Location of the TLS private key (key file)")
	insecure := flag.Bool("insecure", false, "Don't verify the TLS certificates of addresses provided as arguments")
	tlsPort := flag.Int("tls-port", 7777, "TLS listen port")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(),
			`Usage:

%s [flags] [address ...]

%s acts as a Sprout relay. It will listen on the port configured by its flags
and will establish Sprout connections to all addresses provided as arguments.

`, os.Args[0], os.Args[0])
		flag.PrintDefaults()
	}
	flag.Parse()

	cert, err := tls.LoadX509KeyPair(*certpath, *keypath)
	if err != nil {
		log.Fatalf("Failed loading certs: %v", err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	tlsConfig.BuildNameToCertificate()

	address := fmt.Sprintf(":%d", *tlsPort)
	listener, err := tls.Listen("tcp", address, tlsConfig)
	if err != nil {
		log.Fatalf("Failed to start TLS listener on address %s: %v", address, err)
	}
	done := make(chan struct{})

	// Set up channel on which to send signal notifications.
	// We must use a buffered channel or risk missing the signal
	// if we're not ready to receive when the signal is sent.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	messages := NewMessageStore()
	defer messages.Destroy()

	go func() {
		workerCount := 0
		go func() {
			time.Sleep(time.Second)
			log.Printf("Launching test connection to verify basic functionality")
			conn, err := tls.Dial("tcp", address, &tls.Config{
				InsecureSkipVerify: true,
			})
			if err != nil {
				log.Printf("Test dial failed: %v", err)
				return
			}
			defer func() {
				if err := conn.Close(); err != nil {
					log.Printf("Failed to close test connection: %v", err)
					return
				}
				log.Printf("Closed test connection")
			}()
			sconn, err := sprout.NewConn(conn)
			if err != nil {
				log.Printf("Failed to create sprout conn from test dial: %v", err)
			}
			log.Printf("Sending version information on test connection")
			if _, err := sconn.SendVersion(); err != nil {
				log.Printf("Failed to send version information from test conn: %v", err)
			}
		}()
		for {
			log.Printf("Waiting for connections...")
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Failed accepting connection: %v", err)
				continue
			}
			worker, err := NewWorker(done, conn, messages)
			if err != nil {
				log.Printf("Failed launching worker: %v", err)
				continue
			}
			worker.Logger = log.New(log.Writer(), fmt.Sprintf("worker-%d ", workerCount), log.Flags())
			go worker.Run()
			log.Printf("Launched worker-%d to handle new connection", workerCount)
			workerCount++
			select {
			case <-done:
				log.Printf("Done channel closed")
				return
			default:
			}
		}
	}()
	for _, address := range flag.Args() {
		var tlsConfig *tls.Config
		if *insecure {
			tlsConfig = &tls.Config{
				InsecureSkipVerify: true,
			}
		}
		conn, err := tls.Dial("tcp", address, tlsConfig)
		if err != nil {
			log.Printf("Failed to connect to %s: %v", address, err)
			continue
		}
		worker, err := NewWorker(done, conn, messages)
		if err != nil {
			log.Printf("Failed launching worker to connect to address %s: %v", address, err)
			continue
		}
		worker.Logger = log.New(log.Writer(), fmt.Sprintf("worker-%v ", address), log.Flags())
		go worker.Run()
	}
	// Block until a signal is received.
	<-c
	close(done)
}
