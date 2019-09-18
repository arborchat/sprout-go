package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	certpath := flag.String("certpath", "", "Location of the TLS public key (certificate file)")
	keypath := flag.String("keypath", "", "Location of the TLS private key (key file)")
	tlsPort := flag.Int("tls-port", 7777, "TLS listen port")
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

	listener, err := tls.Listen("tcp", fmt.Sprintf(":%d", *tlsPort), tlsConfig)
	if err != nil {
		log.Fatalf("Failed to start TLS listener on port %d: %v", *tlsPort, err)
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Failed accepting connection: %v", err)
		}
		go io.Copy(os.Stdout, conn)
	}
}
