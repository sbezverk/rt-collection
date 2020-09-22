package arangodb

import (
	"context"
	"fmt"
	"strings"

	driver "github.com/arangodb/go-driver"
	"github.com/golang/glog"
	"github.com/sbezverk/gobmp/pkg/bgp"
	"github.com/sbezverk/gobmp/pkg/message"
	notifier "github.com/sbezverk/topology/pkg/kafkanotifier"
)

// L3VPNRT defines route target record
type L3VPNRT struct {
	ID       string            `json:"_id,omitempty"`
	Key      string            `json:"_key,omitempty"`
	Rev      string            `json:"_rev,omitempty"`
	RT       string            `json:"RT,omitempty"`
	Prefixes map[string]string `json:"Prefixes,omitempty"`
}

func (a *arangoDB) l3vpnHandler(obj *notifier.EventMessage) error {
	ctx := context.TODO()
	if obj == nil {
		return fmt.Errorf("event message is nil")
	}
	// Check if Collection encoded in ID exists
	c := strings.Split(obj.ID, "/")[0]
	if strings.Compare(c, a.l3vpn.Name()) != 0 {
		return fmt.Errorf("configured collection name %s and received in event collection name %s do not match", a.l3vpn.Name(), c)
	}
	glog.V(5).Infof("Processing action: %s for prefix: %s", obj.Action, obj.Key)
	var o message.L3VPNPrefix
	_, err := a.l3vpn.ReadDocument(ctx, obj.Key, &o)
	if err != nil {
		return fmt.Errorf("failed to read existing document %s with error: %+v", obj.Key, err)
	}
	switch obj.Action {
	case "add":
		if err := a.processAddRouteTargets(ctx, obj.Key, obj.ID, o.BaseAttributes.ExtCommunityList); err != nil {
			return fmt.Errorf("failed to update the route target collection %s with reference to %s with error: %+v", a.rt.Name(), obj.Key, err)
		}
	case "update":
		if err := a.processUpdateRouteTargets(ctx, obj.Key, obj.ID, o.BaseAttributes.ExtCommunityList); err != nil {
			return fmt.Errorf("failed to update the route target collection %s with reference to %s with error: %+v", a.rt.Name(), obj.Key, err)
		}
	case "del":
		if err := a.processDeleteRouteTargets(ctx, obj.Key, obj.ID); err != nil {
			return fmt.Errorf("failed to clean up the route target collection %s from references to %s with error: %+v", a.rt.Name(), obj.Key, err)
		}
	}
	return nil
}

func (a *arangoDB) processAddRouteTargets(ctx context.Context, key, id string, extCommunities []string) error {
	for _, ext := range extCommunities {
		if !strings.HasPrefix(ext, bgp.ECPRouteTarget) {
			continue
		}
		v := strings.TrimPrefix(ext, bgp.ECPRouteTarget)
		if err := a.processRTAdd(ctx, id, key, v); err != nil {
			return err
		}
	}

	return nil
}

func (a *arangoDB) processDeleteRouteTargets(ctx context.Context, key, id string) error {
	rts, ok := a.store[key]
	if !ok {
		return nil
	}
	for _, rt := range rts {
		if err := a.processRTDel(ctx, id, key, rt); err != nil {
			return err
		}
	}
	delete(a.store, key)

	return nil
}

func (a *arangoDB) processUpdateRouteTargets(ctx context.Context, key, id string, extCommunities []string) error {
	erts, ok := a.store[key]
	if !ok {
		return fmt.Errorf("attempting to update non existing in the store prefix: %s", key)
	}
	nrts := make([]string, len(extCommunities))
	for i, rt := range extCommunities {
		if !strings.HasPrefix(rt, bgp.ECPRouteTarget) {
			continue
		}
		v := strings.TrimPrefix(rt, bgp.ECPRouteTarget)
		nrts[i] = v
	}
	toAdd, toDel := ExtCommGetDiff("", erts, nrts)

	for _, rt := range toAdd {
		if err := a.processRTAdd(ctx, id, key, rt); err != nil {
			return err
		}
	}
	for _, rt := range toDel {
		if err := a.processRTDel(ctx, id, key, rt); err != nil {
			return err
		}
	}
	rts, ok := a.store[key]
	if !ok {
		return fmt.Errorf("update corrupted existing entry in the store prefix: %s", key)
	}
	if len(rts) == 0 {
		delete(a.store, key)
	}

	return nil
}
func (a *arangoDB) processRTAdd(ctx context.Context, id, key, v string) error {
	found, err := a.rt.DocumentExists(ctx, v)
	if err != nil {
		return err
	}
	rtr := &L3VPNRT{}
	nctx := driver.WithWaitForSync(ctx)
	if found {
		_, err := a.rt.ReadDocument(nctx, v, rtr)
		if err != nil {
			return err
		}
		if _, ok := rtr.Prefixes[id]; ok {
			return nil
		}
		rtr.Prefixes[id] = key
		_, err = a.rt.UpdateDocument(nctx, v, rtr)
		if err != nil {
			return err
		}
	} else {
		rtr.ID = a.rt.Name() + "/" + v
		rtr.Key = v
		rtr.RT = v
		rtr.Prefixes = map[string]string{
			id: key,
		}
		if _, err := a.rt.CreateDocument(nctx, rtr); err != nil {
			return err
		}
	}
	// Updating store with new  prefix - rt entry
	rts, ok := a.store[key]
	if !ok {
		rts = make([]string, 1)
	}
	rts = append(rts, v)
	a.store[key] = rts

	return nil
}

func (a *arangoDB) processRTDel(ctx context.Context, id, key, v string) error {
	found, err := a.rt.DocumentExists(ctx, v)
	if err != nil {
		return err
	}
	rtr := &L3VPNRT{}
	nctx := driver.WithWaitForSync(ctx)
	if !found {
		return nil
	}
	if _, err := a.rt.ReadDocument(nctx, v, rtr); err != nil {
		return err
	}
	if _, ok := rtr.Prefixes[id]; !ok {
		return nil
	}
	delete(rtr.Prefixes, id)
	// Check If route target document has any references to other prefixes, if no, then deleting
	// Route Target document, otherwise updating it
	if len(rtr.Prefixes) == 0 {
		glog.Infof("RT with key %s has no more entries, deleting it...", v)
		_, err := a.rt.RemoveDocument(ctx, v)
		if err != nil {
			return fmt.Errorf("failed to delete empty route target %s with error: %+v", v, err)
		}
		return nil
	}
	_, err = a.rt.UpdateDocument(nctx, v, rtr)
	if err != nil {
		return err
	}
	// Updating store with new  prefix - rt entry
	rts, ok := a.store[key]
	if ok {
		for i, rt := range rts {
			if strings.Compare(rt, v) == 0 {
				rts = append(rts[:i], rts[i+1:]...)
				break
			}
		}
	}
	a.store[key] = rts

	return nil
}

// ExtCommGetDiff checks two sets of extended communities for differences and returns two slices.
// First slice (toAdd) carries items which were not in the old but are in the new, second
// slice carries items which were in old but absent in the new.
// extCommType carries a prefix of a particular type of extended community,
// see github.com/sbezverk/gobmp/pkg/bgp/extended-community.gp for definitions.
func ExtCommGetDiff(extCommType string, old, new []string) ([]string, []string) {
	toDel := diffSlice(extCommType, old, new)
	toAdd := diffSlice(extCommType, new, old)

	return toAdd, toDel
}

func diffSlice(prefix string, s1, s2 []string) []string {
	diff := make([]string, 0)
	for i, s1e := range s1 {
		found := false
		if !strings.HasPrefix(s1e, prefix) {
			continue
		}
		// oe = strings.TrimPrefix(oe, extCommType)
		for _, s2e := range s2 {
			if !strings.HasPrefix(s2e, prefix) {
				continue
			}
			// ne = strings.TrimPrefix(ne, extCommType)
			if strings.Compare(s1e, s2e) == 0 {
				found = true
				break
			}
		}
		if !found {
			diff = append(diff, s1[i])
		}
	}

	return diff
}
