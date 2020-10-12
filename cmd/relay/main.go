package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"time"

	_ "net/http/pprof"

	"git.sr.ht/~athorp96/forest-ex/expiration"
	"git.sr.ht/~whereswaldon/forest-go"
	"git.sr.ht/~whereswaldon/forest-go/grove"
	"git.sr.ht/~whereswaldon/forest-go/store"
	sprout "git.sr.ht/~whereswaldon/sprout-go"
	"git.sr.ht/~whereswaldon/sprout-go/watch"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func tickerChan(seconds int) <-chan time.Time {
	return time.NewTicker(time.Second * time.Duration(seconds)).C
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	wd, _ := os.Getwd()
	grovePath := flag.String("grovepath", wd, "Location of the grove of Arbor Nodes to use")
	certpath := flag.String("certpath", "", "Location of the TLS public key (certificate file)")
	keypath := flag.String("keypath", "", "Location of the TLS private key (key file)")
	insecure := flag.Bool("insecure", false, "Don't verify the TLS certificates of addresses provided as arguments")
	tlsPort := flag.Int("tls-port", 7777, "TLS listen port")
	tlsIP := flag.String("tls-ip", "127.0.0.1", "TLS listen IP address")
	profilingAddr := flag.String("profiling-addr", "", "pprof http profiling address")
	metricsAddress := flag.String("metrics-address", "127.0.0.1:2112", "Address on which to serve prometheus metrics")
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

	if *profilingAddr != "" {
		log.Printf("Serving pprof on %s", *profilingAddr)
		go func() { log.Println(http.ListenAndServe(*profilingAddr, nil)) }()
	}

	cert, err := tls.LoadX509KeyPair(*certpath, *keypath)
	if err != nil {
		log.Fatalf("Failed loading certs: %v", err)
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}
	tlsConfig.BuildNameToCertificate()

	listenAddress := fmt.Sprintf("%s:%d", *tlsIP, *tlsPort)
	log.Printf("Listening on %s", listenAddress)
	listener, err := tls.Listen("tcp", listenAddress, tlsConfig)
	if err != nil {
		log.Fatalf("Failed to start TLS listener on address %s: %v", listenAddress, err)
	}
	done := make(chan struct{})

	if *metricsAddress != "" {
		go func() {
			http.Handle("/metrics", promhttp.Handler())
			http.ListenAndServe(*metricsAddress, nil)
		}()
	}

	// Set up channel on which to send signal notifications.
	// We must use a buffered channel or risk missing the signal
	// if we're not ready to receive when the signal is sent.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)

	grove, err := grove.New(*grovePath)
	if err != nil {
		log.Fatalf("Failed to create grove at %s: %v", *grovePath, err)
	}
	messages := store.NewArchive(grove)
	defer messages.Destroy()

	// periodically check for expired nodes and remove them from storage
	expiration.ExpiredPurger{
		PurgeInterval: time.Hour,
		Logger:        log.New(log.Writer(), "purge", log.Flags()),
		ExtendedStore: messages,
	}.Start(done)

	// track node ids of nodes that we've recently inserted into the grove so that
	// we know when a new FS write was us or another process
	addedOurselves := NewExpiringSet()

	// subscribe to added messges so that we can filter them out when we get their
	// FS watching notifications
	messages.PresubscribeToNewMessages(func(n forest.Node) {
		addedOurselves.Add(n.ID().String(), time.Minute)
	})

	watchLogger := log.New(log.Writer(), "watch", log.LstdFlags|log.Lshortfile)
	_, err = watch.Watch(*grovePath, watchLogger, func(filename string) {
		data, err := ioutil.ReadFile(filename)
		if err != nil {
			watchLogger.Printf("Failed reading watched file: %s: %v", filename, err)
			return
		}
		node, err := forest.UnmarshalBinaryNode(data)
		if err != nil {
			watchLogger.Printf("Failed unmarshalling watched file: %s: %v", filename, err)
			return
		}
		if ok := addedOurselves.Has(node.ID().String()); ok {
			watchLogger.Printf("Ignoring node %s because we wrote it", node.ID())
			return
		}
		if err := node.ValidateDeep(messages); err != nil {
			watchLogger.Printf("Failed validating node from watched file: %s: %v", filename, err)
			return

		}
		if err := messages.Add(node); err != nil {
			watchLogger.Printf("Failed adding node from watched file: %s: %v", filename, err)
			return
		}
	})

	// start listening for new connections
	go func() {
		workerCount := 0
		for {
			log.Printf("Waiting for connections...")
			conn, err := listener.Accept()
			if err != nil {
				log.Printf("Failed accepting connection: %v", err)
				continue
			}
			worker, err := sprout.NewWorker(done, conn, messages)
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

	var peerTlsConfig *tls.Config
	if *insecure {
		peerTlsConfig = &tls.Config{
			InsecureSkipVerify: true,
		}
	}
	// dial peers mentioned in arguments
	for _, address := range flag.Args() {
		sprout.LaunchSupervisedWorker(done, address, messages, peerTlsConfig, log.New(log.Writer(), "", log.Flags()))
	}

	// Block until a signal is received.
	<-c
	close(done)
}
