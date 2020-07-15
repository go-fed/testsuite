package server

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/go-fed/activity/pub"
	"github.com/go-fed/activity/streams/vocab"
)

var _ pub.CommonBehavior = &Actor{}
var _ pub.SocialProtocol = &Actor{}
var _ pub.FederatingProtocol = &Actor{}

type Actor struct {
	db *Database
	am *ActorMapping
	tr *TestRunner
}

func NewActor(db *Database, am *ActorMapping, tr *TestRunner) *Actor {
	return &Actor{
		db: db,
		am: am,
		tr: tr,
	}
}

/* COMMON BEHAVIORS */

func (a *Actor) AuthenticateGetInbox(c context.Context, w http.ResponseWriter, r *http.Request) (out context.Context, authenticated bool, err error) {
	authenticated = true
	a.tr.LogAuthenticateGetInbox(c, w, r, authenticated, err)
	return
}

func (a *Actor) AuthenticateGetOutbox(c context.Context, w http.ResponseWriter, r *http.Request) (out context.Context, authenticated bool, err error) {
	authenticated = true
	a.tr.LogAuthenticateGetOutbox(c, w, r, authenticated, err)
	return
}

func (a *Actor) GetOutbox(c context.Context, r *http.Request) (p vocab.ActivityStreamsOrderedCollectionPage, err error) {
	id := HTTPRequestToIRI(r)
	p, err = a.db.GetOutbox(c, id)
	a.tr.LogGetOutbox(c, r, id, p, err)
	return
}

func (a *Actor) NewTransport(c context.Context, actorBoxIRI *url.URL, gofedAgent string) (t pub.Transport, err error) {
	t, err = HTTPSigTransport(c, a.am)
	a.tr.LogNewTransport(c, actorBoxIRI, err)
	return
}

func (a *Actor) DefaultCallback(c context.Context, activity pub.Activity) error {
	a.tr.LogDefaultCallback(c, activity)
	return nil
}

/* SOCIAL PROTOCOL */

func (a *Actor) PostOutboxRequestBodyHook(c context.Context, r *http.Request, data vocab.Type) (context.Context, error) {
	a.tr.LogPostOutboxRequestBodyHook(c, r, data)
	return c, nil
}

func (a *Actor) AuthenticatePostOutbox(c context.Context, w http.ResponseWriter, r *http.Request) (out context.Context, authenticated bool, err error) {
	authenticated = true
	a.tr.LogAuthenticatePostOutbox(c, w, r, authenticated, err)
	return
}

func (a *Actor) SocialCallbacks(c context.Context) (wrapped pub.SocialWrappedCallbacks, other []interface{}, err error) {
	wrapped = pub.SocialWrappedCallbacks{
		Create: func(c context.Context, v vocab.ActivityStreamsCreate) error {
			a.tr.LogSocialCreate(c, v)
			return nil
		},
		Update: func(c context.Context, v vocab.ActivityStreamsUpdate) error {
			a.tr.LogSocialUpdate(c, v)
			return nil
		},
		Delete: func(c context.Context, v vocab.ActivityStreamsDelete) error {
			a.tr.LogSocialDelete(c, v)
			return nil
		},
		Follow: func(c context.Context, v vocab.ActivityStreamsFollow) error {
			a.tr.LogSocialFollow(c, v)
			return nil
		},
		Add: func(c context.Context, v vocab.ActivityStreamsAdd) error {
			a.tr.LogSocialAdd(c, v)
			return nil
		},
		Remove: func(c context.Context, v vocab.ActivityStreamsRemove) error {
			a.tr.LogSocialRemove(c, v)
			return nil
		},
		Like: func(c context.Context, v vocab.ActivityStreamsLike) error {
			a.tr.LogSocialLike(c, v)
			return nil
		},
		Undo: func(c context.Context, v vocab.ActivityStreamsUndo) error {
			a.tr.LogSocialUndo(c, v)
			return nil
		},
		Block: func(c context.Context, v vocab.ActivityStreamsBlock) error {
			a.tr.LogSocialBlock(c, v)
			return nil
		},
	}
	return
}

/* FEDERATING PROTOCOL */

func (a *Actor) PostInboxRequestBodyHook(c context.Context, r *http.Request, activity pub.Activity) (context.Context, error) {
	a.am.AddContextInfo(c, r)
	a.tr.LogPostInboxRequestBodyHook(c, r, activity)
	return c, nil
}

func (a *Actor) AuthenticatePostInbox(c context.Context, w http.ResponseWriter, r *http.Request) (out context.Context, authenticated bool, err error) {
	out = c
	client := &http.Client{
		Timeout: time.Second * 30,
	}
	authenticated, err = verifyHttpSignatures(c, a.db.hostname, client, r, a.am)
	a.tr.LogAuthenticatePostInbox(c, w, r, authenticated, err)
	return
}

func (a *Actor) Blocked(c context.Context, actorIRIs []*url.URL) (blocked bool, err error) {
	blocked = false
	a.tr.LogBlocked(c, actorIRIs, blocked, err)
	return
}

func (a *Actor) FederatingCallbacks(c context.Context) (wrapped pub.FederatingWrappedCallbacks, other []interface{}, err error) {
	wrapped = pub.FederatingWrappedCallbacks{
		OnFollow: pub.OnFollowDoNothing,
		Create: func(c context.Context, v vocab.ActivityStreamsCreate) error {
			a.tr.LogFederatingCreate(c, v)
			return nil
		},
		Update: func(c context.Context, v vocab.ActivityStreamsUpdate) error {
			a.tr.LogFederatingUpdate(c, v)
			return nil
		},
		Delete: func(c context.Context, v vocab.ActivityStreamsDelete) error {
			a.tr.LogFederatingDelete(c, v)
			return nil
		},
		Follow: func(c context.Context, v vocab.ActivityStreamsFollow) error {
			a.tr.LogFederatingFollow(c, v)
			return nil
		},
		Add: func(c context.Context, v vocab.ActivityStreamsAdd) error {
			a.tr.LogFederatingAdd(c, v)
			return nil
		},
		Remove: func(c context.Context, v vocab.ActivityStreamsRemove) error {
			a.tr.LogFederatingRemove(c, v)
			return nil
		},
		Like: func(c context.Context, v vocab.ActivityStreamsLike) error {
			a.tr.LogFederatingLike(c, v)
			return nil
		},
		Undo: func(c context.Context, v vocab.ActivityStreamsUndo) error {
			a.tr.LogFederatingUndo(c, v)
			return nil
		},
		Block: func(c context.Context, v vocab.ActivityStreamsBlock) error {
			a.tr.LogFederatingBlock(c, v)
			return nil
		},
	}
	return
}

func (a *Actor) MaxInboxForwardingRecursionDepth(c context.Context) int {
	return 3
}

func (a *Actor) MaxDeliveryRecursionDepth(c context.Context) int {
	return 3
}

func (a *Actor) FilterForwarding(c context.Context, potentialRecipients []*url.URL, activity pub.Activity) (filteredRecipients []*url.URL, err error) {
	// Filter out everyone.
	a.tr.LogFilterForwarding(c, potentialRecipients, activity, filteredRecipients, err)
	return
}

func (a *Actor) GetInbox(c context.Context, r *http.Request) (p vocab.ActivityStreamsOrderedCollectionPage, err error) {
	id := HTTPRequestToIRI(r)
	p, err = a.db.GetInbox(c, id)
	a.tr.LogGetInbox(c, r, id, p, err)
	return
}
