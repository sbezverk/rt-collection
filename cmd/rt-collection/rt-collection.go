package main

import (
	"flag"
	"os"
	"os/signal"
	"runtime"

	"github.com/golang/glog"
	"github.com/sbezverk/rt-collection/pkg/arangodb"
	"github.com/sbezverk/rt-collection/pkg/kafkamessenger"

	_ "net/http/pprof"
)

var (
	msgSrvAddr      string
	dbSrvAddr       string
	dbName          string
	dbUser          string
	dbPass          string
	l3vpnCollection string
	rtCollection    string
)

func init() {
	runtime.GOMAXPROCS(1)
	flag.StringVar(&msgSrvAddr, "message-server", "", "URL to the messages supplying server")
	flag.StringVar(&dbSrvAddr, "database-server", "", "{dns name}:port or X.X.X.X:port of the graph database")
	flag.StringVar(&dbName, "database-name", "", "DB name")
	flag.StringVar(&dbUser, "database-user", "", "DB User name")
	flag.StringVar(&dbPass, "database-pass", "", "DB User's password")
	flag.StringVar(&l3vpnCollection, "l3vpn-collection", "L3VPNV4_Prefix_Test", "Collection name for L3VPN prefixes, default: \"L3VPNV4_Prefix_Test\"")
	flag.StringVar(&rtCollection, "rt-collection", "RT_L3VPNV4_Test", "Collection name for L3VPN Route Targets, default \"RT_L3VPNV4_Test\"")
}

var (
	onlyOneSignalHandler = make(chan struct{})
	shutdownSignals      = []os.Signal{os.Interrupt}
)

func setupSignalHandler() (stopCh <-chan struct{}) {
	close(onlyOneSignalHandler) // panics when called twice

	stop := make(chan struct{})
	c := make(chan os.Signal, 2)
	signal.Notify(c, shutdownSignals...)
	go func() {
		<-c
		close(stop)
		<-c
		os.Exit(1) // second signal. Exit directly.
	}()

	return stop
}

func main() {
	flag.Parse()
	_ = flag.Set("logtostderr", "true")

	dbSrv, err := arangodb.NewDBSrvClient(dbSrvAddr, dbUser, dbPass, dbName, l3vpnCollection, rtCollection)
	if err != nil {
		glog.Errorf("failed to initialize databse client with error: %+v", err)
		os.Exit(1)
	}

	if err := dbSrv.Start(); err != nil {
		if err != nil {
			glog.Errorf("failed to connect to database with error: %+v", err)
			os.Exit(1)
		}
	}

	// Initializing messenger process
	msgSrv, err := kafkamessenger.NewKafkaMessenger(msgSrvAddr, dbSrv.GetInterface())
	if err != nil {
		glog.Errorf("failed to initialize message server with error: %+v", err)
		os.Exit(1)
	}

	msgSrv.Start()

	stopCh := setupSignalHandler()
	<-stopCh

	msgSrv.Stop()
	dbSrv.Stop()

	os.Exit(0)
}
