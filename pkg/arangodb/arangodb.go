package arangodb

import (
	"context"
	"encoding/json"
	"fmt"

	driver "github.com/arangodb/go-driver"
	"github.com/golang/glog"
	"github.com/sbezverk/gobmp/pkg/bmp"
	"github.com/sbezverk/gobmp/pkg/message"
	"github.com/sbezverk/gobmp/pkg/tools"
	"github.com/sbezverk/rt-collection/pkg/dbclient"
	notifier "github.com/sbezverk/topology/pkg/kafkanotifier"
)

const (
	concurrentWorkers = 1024
	l3vpnCollection   = "L3VPN_Prefix_Test"
	rtCollection      = "RT_L3VPN_Test"
)

type arangoDB struct {
	dbclient.DB
	*ArangoConn
	stop  chan struct{}
	l3vpn driver.Collection
	rt    driver.Collection
	store map[string][]string
}

// NewDBSrvClient returns an instance of a DB server client process
func NewDBSrvClient(arangoSrv, user, pass, dbname string) (dbclient.Srv, error) {
	if err := tools.URLAddrValidation(arangoSrv); err != nil {
		return nil, err
	}
	arangoConn, err := NewArango(ArangoConfig{
		URL:      arangoSrv,
		User:     user,
		Password: pass,
		Database: dbname,
	})
	if err != nil {
		return nil, err
	}
	arango := &arangoDB{
		stop:  make(chan struct{}),
		store: make(map[string][]string),
	}
	arango.DB = arango
	arango.ArangoConn = arangoConn

	// Check if L3VPN collection exists, if not fail as Jalapeno topology is not running
	arango.l3vpn, err = arango.db.Collection(context.TODO(), l3vpnCollection)
	if err != nil {
		return nil, err
	}

	found, err := arango.db.CollectionExists(context.TODO(), rtCollection)
	if err != nil {
		return nil, err
	}
	if found {
		c, err := arango.db.Collection(context.TODO(), rtCollection)
		if err != nil {
			return nil, err
		}
		if err := c.Remove(context.TODO()); err != nil {
			return nil, err
		}
	}
	arango.rt, err = arango.db.CreateCollection(context.TODO(), rtCollection, &driver.CreateCollectionOptions{})
	if err != nil {
		return nil, err
	}

	return arango, nil
}

func (a *arangoDB) Start() error {
	if err := a.loadStore(); err != nil {
		return err
	}
	glog.Infof("Connected to arango database, starting monitor")
	go a.monitor()

	return nil
}

func (a *arangoDB) Stop() error {
	close(a.stop)

	return nil
}

func (a *arangoDB) GetInterface() dbclient.DB {
	return a.DB
}

func (a *arangoDB) GetArangoDBInterface() *ArangoConn {
	return a.ArangoConn
}

func (a *arangoDB) ProcessMessage(msgType int, msg []byte) error {
	if msgType != bmp.L3VPNMsg {
		return fmt.Errorf("unsupported message type %d", msgType)
	}
	event := &notifier.EventMessage{}
	if err := json.Unmarshal(msg, event); err != nil {
		return err
	}

	return a.l3vpnHandler(event)
}

func (a *arangoDB) monitor() {
	for {
		select {
		case <-a.stop:
			// TODO Add clean up of connection with Arango DB
			return
		}
	}
}

func (a *arangoDB) loadStore() error {
	ctx := context.TODO()
	query := "FOR d IN " + a.l3vpn.Name() + " RETURN d"
	cursor, err := a.db.Query(ctx, query, nil)
	if err != nil {
		return err
	}
	defer cursor.Close()
	for {
		var p message.L3VPNPrefix
		meta, err := cursor.ReadDocument(ctx, &p)
		if driver.IsNoMoreDocuments(err) {
			break
		} else if err != nil {
			return err
		}
		if err := a.processAddRouteTargets(ctx, meta.Key, meta.ID.String(), p.BaseAttributes.ExtCommunityList); err != nil {
			return err
		}
	}
	glog.Infof("Store after initialization: %+v", a.store)

	return nil
}
