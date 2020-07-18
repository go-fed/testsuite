package server

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"fmt"
	mathrand "math/rand"
	"net/http"
	"net/url"
	"path"
	"strings"
)

const (
	kContextKeyRequestPath = "ckrp"
)

/* Web mappings */

func PathToTestPathPrefix(u *url.URL) (s string, ok bool) {
	remain := strings.TrimPrefix(
		strings.TrimPrefix(u.Path, kPathPrefixTests),
		"/")
	var id string
	id, ok = testIdFromRemainingPath(remain)
	if !ok {
		return
	}
	s = testPathPrefixFromId(id)
	return
}

func StatePathToTestPathPrefix(u *url.URL) (s string, ok bool) {
	remain := strings.TrimPrefix(
		strings.TrimPrefix(u.Path, kPathTestState),
		"/")
	var id string
	id, ok = testIdFromRemainingPath(remain)
	if !ok {
		return
	}
	s = testPathPrefixFromId(id)
	return
}

func InstructionResponsePathToTestPathPrefix(u *url.URL) (s string, ok bool) {
	remain := strings.TrimPrefix(
		strings.TrimPrefix(u.Path, kPathInstructionResponse),
		"/")
	var id string
	id, ok = testIdFromRemainingPath(remain)
	if !ok {
		return
	}
	s = testPathPrefixFromId(id)
	return
}

func InstructionResponsePathToTestState(u *url.URL) (s string, ok bool) {
	remain := strings.TrimPrefix(
		strings.TrimPrefix(u.Path, kPathInstructionResponse),
		"/")
	var id string
	id, ok = testIdFromRemainingPath(remain)
	if !ok {
		return
	}
	s = path.Join(kPathTestState, id)
	return
}

func testIdFromPathPrefix(pathPrefix string) (id string) {
	id = strings.TrimPrefix(
		strings.TrimPrefix(pathPrefix, kPathPrefixTests),
		"/")
	return
}

func testPathPrefixFromId(id string) string {
	return path.Join(kPathPrefixTests, id)
}

func testIdFromRemainingPath(remain string) (s string, ok bool) {
	ok = true
	parts := strings.Split(remain, "/")
	if len(parts) < 1 {
		ok = false
		return
	}
	s = parts[0]
	return
}

/* ActivityPub Mappings */

type KeyData struct {
	PubKeyID  string
	PubKeyURL string
	PrivKey   crypto.PrivateKey
}

type ActorMapping struct {
	inboxToKeyData map[string]KeyData
}

func NewActorMapping() *ActorMapping {
	return &ActorMapping{
		inboxToKeyData: make(map[string]KeyData),
	}
}

func (a *ActorMapping) generateKeyData(actor *url.URL) (kd KeyData, err error) {
	var rsaKey crypto.PrivateKey
	rsaKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return
	}
	kd = KeyData{
		PubKeyID:  "pubKeyFoo",
		PubKeyURL: ActorIRIToPubKeyURL(actor).String(),
		PrivKey:   rsaKey,
	}
	a.inboxToKeyData[ActorIRIToInboxIRI(actor).Path] = kd
	return
}

func (a *ActorMapping) AddContextInfo(c context.Context, r *http.Request) context.Context {
	c = context.WithValue(c, kContextKeyRequestPath, r.URL.Path)
	return c
}

func (a *ActorMapping) GetKeyInfo(c context.Context) (pubKeyID, pubKeyURL string, privKey crypto.PrivateKey, err error) {
	path, ok := c.Value(kContextKeyRequestPath).(string)
	if !ok {
		err = fmt.Errorf("cannot get request path from context in GetKeyInfo")
		return
	}
	kd, ok := a.inboxToKeyData[path]
	if !ok {
		err = fmt.Errorf("cannot get keydata for %s", path)
		return
	}
	pubKeyID = kd.PubKeyID
	pubKeyURL = kd.PubKeyURL
	privKey = kd.PrivKey
	return
}

/* Well-known AP endpoints mapping:
Actor:     /tests/evaluate/123/actors/<name>
Inbox:     /tests/evaluate/123/actors/<name>/inbox
Outbox:    /tests/evaluate/123/actors/<name>/outbox
Following: /tests/evaluate/123/actors/<name>/following
Followers: /tests/evaluate/123/actors/<name>/followers
Liked:     /tests/evaluate/123/actors/<name>/liked
Other:     /tests/evaluate/123/other/<type>/456
*/

const (
	kUp        = ".."
	kOutbox    = "outbox"
	kInbox     = "inbox"
	kOther     = "other"
	kFollowers = "followers"
	kFollowing = "following"
	kLiked     = "liked"
)

func OutboxIRIToActorIRI(outbox *url.URL) *url.URL {
	c, _ := url.Parse(outbox.String())
	c.Path = path.Clean(path.Join(c.Path, kUp))
	return c
}

func InboxIRIToActorIRI(inbox *url.URL) *url.URL {
	c, _ := url.Parse(inbox.String())
	c.Path = path.Clean(path.Join(c.Path, kUp))
	return c
}

func OutboxIRIToInboxIRI(outbox *url.URL) *url.URL {
	c, _ := url.Parse(outbox.String())
	c.Path = path.Clean(path.Join(c.Path, kUp, kInbox))
	return c
}

func NewIDPath(pathPrefix string, typename string) string {
	testNumber := mathrand.Int()
	return path.Join(pathPrefix, kOther, strings.ToLower(typename), fmt.Sprintf("%d", testNumber))
}

func NewPathWithIndex(pathPrefix string, typename string, reason string, idx int) string {
	return path.Join(pathPrefix, kOther, strings.ToLower(typename), reason, fmt.Sprintf("%d", idx))
}

func ActorIRIToInboxIRI(actor *url.URL) *url.URL {
	c, _ := url.Parse(actor.String())
	c.Path = path.Join(c.Path, kInbox)
	return c
}

func ActorIRIToOutboxIRI(actor *url.URL) *url.URL {
	c, _ := url.Parse(actor.String())
	c.Path = path.Join(c.Path, kOutbox)
	return c
}

func ActorIRIToFollowersIRI(actor *url.URL) *url.URL {
	c, _ := url.Parse(actor.String())
	c.Path = path.Join(c.Path, kFollowers)
	return c
}

func ActorIRIToFollowingIRI(actor *url.URL) *url.URL {
	c, _ := url.Parse(actor.String())
	c.Path = path.Join(c.Path, kFollowing)
	return c
}

func ActorIRIToLikedIRI(actor *url.URL) *url.URL {
	c, _ := url.Parse(actor.String())
	c.Path = path.Join(c.Path, kLiked)
	return c
}

func ActorIRIToPubKeyURL(actor *url.URL) *url.URL {
	c, _ := url.Parse(actor.String())
	c.Fragment = "pubKeyFoo"
	return c
}

func IsRelativePathToInboxIRI(path string) bool {
	return strings.HasSuffix(path, kInbox)
}

func IsRelativePathToOutboxIRI(path string) bool {
	return strings.HasSuffix(path, kOutbox)
}

func HTTPRequestToIRI(r *http.Request) *url.URL {
	id := &url.URL{}
	id.Path = r.URL.Path
	id.Host = r.Host
	id.Scheme = "https"
	return id
}
