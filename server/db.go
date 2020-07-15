package server

import (
	"context"
	"fmt"
	"net/url"
	"sync"

	"github.com/go-fed/activity/pub"
	"github.com/go-fed/activity/streams"
	"github.com/go-fed/activity/streams/vocab"
)

const (
	kContextKeyTestPrefix = "ctxtp"
)

var _ pub.Database = &Database{}

type Database struct {
	hostname string
	// Database structures
	content       map[interface{}]vocab.Type
	contentFineMu map[interface{}]*sync.Mutex
	contentMu     sync.RWMutex
}

func NewDatabase(hostname string) *Database {
	return &Database{
		hostname:      hostname,
		content:       make(map[interface{}]vocab.Type, 0),
		contentFineMu: make(map[interface{}]*sync.Mutex, 0),
		contentMu:     sync.RWMutex{},
	}
}

func (d *Database) Lock(c context.Context, id *url.URL) error {
	d.contentMu.RLock()
	m, ok := d.contentFineMu[id.String()]
	d.contentMu.RUnlock()
	if ok {
		m.Lock()
	} else {
		// Not good enough, but we'll cross our fingers.
		d.contentMu.Lock()
		m = &sync.Mutex{}
		d.contentFineMu[id.String()] = m
		d.contentMu.Unlock()
		m.Lock()
	}
	return nil
}

func (d *Database) Unlock(c context.Context, id *url.URL) error {
	d.contentMu.RLock()
	m, ok := d.contentFineMu[id.String()]
	d.contentMu.RUnlock()
	if ok {
		m.Unlock()
		return nil
	} else {
		return fmt.Errorf("could not find mutex to unlock: %s", id)
	}
}

func (d *Database) InboxContains(c context.Context, inbox, id *url.URL) (contains bool, err error) {
	var tr *streams.TypeResolver
	tr, err = streams.NewTypeResolver(func(c context.Context, oc vocab.ActivityStreamsOrderedCollection) error {
		oi := oc.GetActivityStreamsOrderedItems()
		if oi != nil {
			for iter := oi.Begin(); iter != oi.End(); iter = iter.Next() {
				oid, err := pub.ToId(iter)
				if err != nil {
					return err
				}
				if oid.String() == id.String() {
					contains = true
					return nil
				}
			}
		}
		return nil
	})
	if err != nil {
		return
	}
	iv, ok := d.content[inbox.String()]
	if !ok {
		err = fmt.Errorf("no inbox at: %s", inbox)
		return
	}
	err = tr.Resolve(c, iv)
	return
}

func (d *Database) GetInbox(c context.Context, inboxIRI *url.URL) (inbox vocab.ActivityStreamsOrderedCollectionPage, err error) {
	inbox, err = d.toOCPageFromOC(c, inboxIRI)
	return
}

func (d *Database) SetInbox(c context.Context, inbox vocab.ActivityStreamsOrderedCollectionPage) error {
	return d.toOCFromOCPage(c, inbox)
}

func (d *Database) Owns(c context.Context, id *url.URL) (owns bool, err error) {
	owns = id.Host == d.hostname
	return
}

func (d *Database) ActorForOutbox(c context.Context, outboxIRI *url.URL) (actorIRI *url.URL, err error) {
	actorIRI = OutboxIRIToActorIRI(outboxIRI)
	return
}

func (d *Database) ActorForInbox(c context.Context, inboxIRI *url.URL) (actorIRI *url.URL, err error) {
	actorIRI = InboxIRIToActorIRI(inboxIRI)
	return
}

func (d *Database) OutboxForInbox(c context.Context, inboxIRI *url.URL) (outboxIRI *url.URL, err error) {
	outboxIRI = OutboxIRIToInboxIRI(inboxIRI)
	return
}

func (d *Database) Exists(c context.Context, id *url.URL) (exists bool, err error) {
	_, exists = d.content[id.String()]
	return
}

func (d *Database) Get(c context.Context, id *url.URL) (value vocab.Type, err error) {
	var ok bool
	value, ok = d.content[id.String()]
	if !ok {
		err = fmt.Errorf("failed to get by id: %s", id)
	}
	return
}

func (d *Database) Create(c context.Context, asType vocab.Type) error {
	id, err := pub.GetId(asType)
	if err != nil {
		return err
	}
	d.content[id.String()] = asType
	return nil
}

func (d *Database) Update(c context.Context, asType vocab.Type) error {
	id, err := pub.GetId(asType)
	if err != nil {
		return err
	}
	d.content[id.String()] = asType
	return nil
}

func (d *Database) Delete(c context.Context, id *url.URL) error {
	delete(d.content, id.String())
	return nil
}

func (d *Database) GetOutbox(c context.Context, outboxIRI *url.URL) (outbox vocab.ActivityStreamsOrderedCollectionPage, err error) {
	outbox, err = d.toOCPageFromOC(c, outboxIRI)
	return
}

func (d *Database) SetOutbox(c context.Context, outbox vocab.ActivityStreamsOrderedCollectionPage) error {
	return d.toOCFromOCPage(c, outbox)
}

func (d *Database) NewID(c context.Context, t vocab.Type) (id *url.URL, err error) {
	prefix, ok := c.Value(kContextKeyTestPrefix).(string)
	if !ok {
		err = fmt.Errorf("cannot determine the test prefix on context for: %s", id)
		return
	}
	idpath := NewIDPath(prefix, t.GetTypeName())
	id = &url.URL{
		Scheme: "https",
		Host:   d.hostname,
		Path:   idpath,
	}
	return
}

func (d *Database) Followers(c context.Context, actorIRI *url.URL) (followers vocab.ActivityStreamsCollection, err error) {
	followers, err = d.toCollectionFromId(c, ActorIRIToFollowersIRI(actorIRI))
	return
}

func (d *Database) Following(c context.Context, actorIRI *url.URL) (following vocab.ActivityStreamsCollection, err error) {
	following, err = d.toCollectionFromId(c, ActorIRIToFollowingIRI(actorIRI))
	return
}

func (d *Database) Liked(c context.Context, actorIRI *url.URL) (liked vocab.ActivityStreamsCollection, err error) {
	liked, err = d.toCollectionFromId(c, ActorIRIToLikedIRI(actorIRI))
	return
}

func (d *Database) toOCPageFromOC(c context.Context, given *url.URL) (result vocab.ActivityStreamsOrderedCollectionPage, err error) {
	var tr *streams.TypeResolver
	tr, err = streams.NewTypeResolver(func(c context.Context, oc vocab.ActivityStreamsOrderedCollection) error {
		result = streams.NewActivityStreamsOrderedCollectionPage()
		poi := streams.NewActivityStreamsOrderedItemsProperty()
		result.SetActivityStreamsOrderedItems(poi)
		// Copy id over to the page
		jid := streams.NewJSONLDIdProperty()
		jiri, err := pub.GetId(oc)
		if err != nil {
			return err
		}
		jid.SetIRI(jiri)
		result.SetJSONLDId(jid)
		// Copy oi to poi
		oi := oc.GetActivityStreamsOrderedItems()
		if oi != nil {
			for iter := oi.Begin(); iter != oi.End(); iter = iter.Next() {
				oid, err := pub.ToId(iter)
				if err != nil {
					return err
				}
				poi.AppendIRI(oid)
			}
		}
		return nil
	})
	if err != nil {
		return
	}
	iv, ok := d.content[given.String()]
	if !ok {
		err = fmt.Errorf("no inbox at: %s", given)
		return
	}
	err = tr.Resolve(c, iv)
	return
}

func (d *Database) toOCFromOCPage(c context.Context, page vocab.ActivityStreamsOrderedCollectionPage) error {
	tr, err := streams.NewTypeResolver(func(c context.Context, oc vocab.ActivityStreamsOrderedCollection) error {
		// Overwrite oc's existing items
		oi := streams.NewActivityStreamsOrderedItemsProperty()
		oc.SetActivityStreamsOrderedItems(oi)
		// Copy poi to oi
		poi := page.GetActivityStreamsOrderedItems()
		if poi != nil {
			for iter := poi.Begin(); iter != poi.End(); iter = iter.Next() {
				poid, err := pub.ToId(iter)
				if err != nil {
					return err
				}
				oi.AppendIRI(poid)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	iri, err := pub.GetId(page)
	if err != nil {
		return err
	}
	iv, ok := d.content[iri.String()]
	if !ok {
		return fmt.Errorf("no inbox at: %s", iri)
	}
	return tr.Resolve(c, iv)
}

func (d *Database) toCollectionFromId(c context.Context, id *url.URL) (col vocab.ActivityStreamsCollection, err error) {
	var tr *streams.TypeResolver
	tr, err = streams.NewTypeResolver(func(c context.Context, co vocab.ActivityStreamsCollection) error {
		col = co
		return nil
	})
	if err != nil {
		return
	}
	iv, ok := d.content[id.String()]
	if !ok {
		err = fmt.Errorf("no inbox at: %s", id)
		return
	}
	err = tr.Resolve(c, iv)
	return

}
