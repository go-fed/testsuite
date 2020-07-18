package server

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/go-fed/activity/pub"
	"github.com/go-fed/activity/streams"
)

const (
	kRecurLimit = 1000
)

type testBundle struct {
	started         time.Time
	enableWebfinger bool
	tr              *TestRunner
	am              *ActorMapping
	pfa             pub.FederatingActor
	ctx             *TestRunnerContext
	db              *Database
	handler         pub.HandlerFunc
	state           *testState
	stateMu         *sync.RWMutex
	timer           *time.Timer
}

type testState struct {
	ID        string
	I         Instruction
	Pending   []TestInfo
	Completed []TestInfo
	Results   []Result
	Err       error
	Done      bool
}

func (t *testState) Clone() *testState {
	ts := &testState{
		Pending:   make([]TestInfo, len(t.Pending)),
		Completed: make([]TestInfo, len(t.Completed)),
		Results:   make([]Result, len(t.Results)),
	}
	copy(ts.Pending, t.Pending)
	copy(ts.Completed, t.Completed)
	copy(ts.Results, t.Results)
	ts.ID = t.ID
	ts.I = t.I
	ts.Err = t.Err
	ts.Done = t.Done
	return ts
}

type TestServer struct {
	hostname    string
	pathParent  string
	testTimeout time.Duration
	maxTests    int
	// Async members
	cache   map[string]testBundle
	cacheMu sync.RWMutex
}

func NewTestServer(hostname, pathParent string, timeout time.Duration, max int) *TestServer {
	return &TestServer{
		hostname:    hostname,
		pathParent:  pathParent,
		testTimeout: timeout,
		maxTests:    max,
		cache:       make(map[string]testBundle, 0),
		cacheMu:     sync.RWMutex{},
	}
}

func (ts *TestServer) StartTest(c context.Context, pathPrefix string, c2s, s2s, enableWebfinger bool, maxDeliverRecur int, testRemoteActorID *url.URL) error {
	started := time.Now().UTC()
	tb := ts.newTestBundle(
		pathPrefix,
		c2s,
		s2s,
		enableWebfinger,
		started,
		testRemoteActorID)

	ok := true
	ts.cacheMu.Lock()
	if len(ts.cache) >= ts.maxTests {
		ok = false
	} else {
		ts.cache[pathPrefix] = tb
	}
	ts.cacheMu.Unlock()
	if !ok {
		return fmt.Errorf("Too many tests ongoing. Please try again in %s.", ts.testTimeout)
	}

	// Prepare our test actor information.
	var err error
	tb.ctx.TestActor0, err = ts.prepareActor(c, tb, pathPrefix, kActor0)
	if err != nil {
		return err
	}
	tb.ctx.TestActor1, err = ts.prepareActor(c, tb, pathPrefix, kActor1)
	if err != nil {
		return err
	}
	tb.ctx.TestActor2, err = ts.prepareActor(c, tb, pathPrefix, kActor2)
	if err != nil {
		return err
	}
	tb.ctx.TestActor3, err = ts.prepareActor(c, tb, pathPrefix, kActor3)
	if err != nil {
		return err
	}
	tb.ctx.TestActor4, err = ts.prepareActor(c, tb, pathPrefix, kActor4)
	if err != nil {
		return err
	}

	if s2s {
		// Prepare nested collections of actors for testing dereference
		// limits during delivery.
		if maxDeliverRecur < 1 {
			return fmt.Errorf("maximum recursion limit for delivery must be >= 1")
		} else if maxDeliverRecur > kRecurLimit {
			maxDeliverRecur = kRecurLimit
			tb.ctx.RecurLimitExceeded = true
		}
		// Iterate from deepest to shallowest collection. The extra
		// collection ensures the 0th one will not be fetched.
		//
		// Set up a series of nested Collections whose members are:
		// {kActor1, kActor2, {kActor3, {...{kActor4}...}}}
		var prevIRI *url.URL
		max := maxDeliverRecur + 1
		var addedActorsLevel2 bool
		for i := 0; i < max; i++ {
			col := streams.NewActivityStreamsCollection()
			id := streams.NewJSONLDIdProperty()
			idIRI := &url.URL{
				Scheme: "https",
				Host:   ts.hostname,
				Path:   NewPathWithIndex(pathPrefix, col.GetTypeName(), "nested", max-i),
			}
			id.Set(idIRI)
			col.SetJSONLDId(id)

			items := streams.NewActivityStreamsItemsProperty()
			if i == 0 {
				items.AppendIRI(tb.ctx.TestActor4.ActivityPubIRI)
			}
			if i >= maxDeliverRecur-1 && !addedActorsLevel2 {
				items.AppendIRI(tb.ctx.TestActor3.ActivityPubIRI)
				addedActorsLevel2 = true // Depending on depth, could happen twice.
			}
			if i >= maxDeliverRecur {
				items.AppendIRI(tb.ctx.TestActor2.ActivityPubIRI)
				items.AppendIRI(tb.ctx.TestActor1.ActivityPubIRI)
			}
			if i > 0 {
				items.AppendIRI(prevIRI)
			}
			prevIRI = idIRI
			col.SetActivityStreamsItems(items)

			if err = tb.db.Create(c, col); err != nil {
				return err
			}
		}
		testID := testIdFromPathPrefix(pathPrefix)
		tb.ctx.RootRecurCollectionID = deliverableIDs{
			ActivityPubIRI:   prevIRI,
			WebfingerId:      fmt.Sprintf("@%s%s%s@%s", kRecurCollection, kWebfingerTestDelim, testID, ts.hostname),
			WebfingerSubject: fmt.Sprintf("%s%s%s", kRecurCollection, kWebfingerTestDelim, testID),
		}
	}

	tb.timer = time.AfterFunc(ts.testTimeout, func() {
		tb.tr.Stop()
		ts.cacheMu.Lock()
		delete(ts.cache, pathPrefix)
		ts.cacheMu.Unlock()
	})
	tb.tr.Run(tb.ctx)
	return nil
}

func (ts *TestServer) HandleWeb(c context.Context, w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, ts.pathParent) {
		http.Error(w, fmt.Sprintf("Cannot HandleWeb for path %s", r.URL.Path), http.StatusInternalServerError)
		return
	}
	remain := strings.TrimPrefix(
		strings.TrimPrefix(r.URL.Path, ts.pathParent),
		"/")
	parts := strings.Split(remain, "/")
	if len(parts) < 2 {
		http.Error(w, fmt.Sprintf("Cannot HandleWeb for path %s", r.URL.Path), http.StatusInternalServerError)
		return
	}
	testID := parts[0]
	testPathPrefix := path.Join(ts.pathParent, testID)

	ts.cacheMu.RLock()
	tb, ok := ts.cache[testPathPrefix]
	ts.cacheMu.RUnlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	relPathPrefix := path.Join(testPathPrefix, parts[1])
	restPath := strings.TrimPrefix(r.URL.Path, relPathPrefix)
	if IsRelativePathToInboxIRI(restPath) {
		if isAP, err := tb.pfa.GetInbox(c, w, r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else if isAP {
			// Nothing, success!
		} else if isAP, err = tb.pfa.PostInbox(c, w, r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else if isAP {
			// Nothing, success!
		} else {
			http.Error(w, "Not an ActivityPub request to the Inbox", http.StatusBadRequest)
		}
	} else if IsRelativePathToOutboxIRI(restPath) {
		if isAP, err := tb.pfa.GetOutbox(c, w, r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else if isAP {
			// Nothing, success!
		} else if isAP, err = tb.pfa.PostOutbox(c, w, r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else if isAP {
			// Nothing, success!
		} else {
			http.Error(w, "Not an ActivityPub request to the Outbox", http.StatusBadRequest)
		}
	} else {
		if isAP, err := tb.handler(c, w, r); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		} else if isAP {
			// Nothing, success!
		} else {
			http.Error(w, "Not an ActivityPub request", http.StatusBadRequest)
		}
	}
}

func (ts *TestServer) TestState(pathPrefix string) (t testState, ok bool) {
	var tb testBundle
	ts.cacheMu.RLock()
	tb, ok = ts.cache[pathPrefix]
	ts.cacheMu.RUnlock()
	if !ok {
		return
	}
	tb.stateMu.RLock()
	if ok {
		t = *tb.state.Clone()
	}
	tb.stateMu.RUnlock()
	return
}

func (ts *TestServer) HandleInstructionResponse(pathPrefix string, vals map[string][]string) {
	ts.cacheMu.RLock()
	tb, ok := ts.cache[pathPrefix]
	ts.cacheMu.RUnlock()
	if !ok {
		return
	}
	tb.stateMu.RLock()
	if ok {
		for k, v := range vals {
			tb.ctx.C = context.WithValue(tb.ctx.C, k, v)
		}
		tb.ctx.InstructionDone()
	}
	tb.stateMu.RUnlock()
}

func (ts *TestServer) HandleWebfinger(pathPrefix string, user string) (username string, apIRI *url.URL, err error) {
	ts.cacheMu.RLock()
	tb, ok := ts.cache[pathPrefix]
	ts.cacheMu.RUnlock()
	if !ok {
		err = fmt.Errorf("test not found for: %s", pathPrefix)
		return
	}
	if !tb.enableWebfinger {
		err = fmt.Errorf("webfinger is not enabled for this test")
		return
	}
	switch user {
	case kActor0:
		apIRI = tb.ctx.TestActor0.ActivityPubIRI
		username = tb.ctx.TestActor0.WebfingerSubject
	case kActor1:
		apIRI = tb.ctx.TestActor1.ActivityPubIRI
		username = tb.ctx.TestActor1.WebfingerSubject
	case kActor2:
		apIRI = tb.ctx.TestActor2.ActivityPubIRI
		username = tb.ctx.TestActor2.WebfingerSubject
	case kActor3:
		apIRI = tb.ctx.TestActor3.ActivityPubIRI
		username = tb.ctx.TestActor3.WebfingerSubject
	case kActor4:
		apIRI = tb.ctx.TestActor4.ActivityPubIRI
		username = tb.ctx.TestActor4.WebfingerSubject
	case kRecurCollection:
		apIRI = tb.ctx.RootRecurCollectionID.ActivityPubIRI
		username = tb.ctx.RootRecurCollectionID.WebfingerSubject
	default:
		err = fmt.Errorf("no webfinger for user with name %s", user)
	}
	return
}

func (ts *TestServer) shutdown() {
	ts.cacheMu.Lock()
	for _, v := range ts.cache {
		v.timer.Stop()
		v.tr.Stop()
	}
	ts.cacheMu.Unlock()
}

func (ts *TestServer) newTestBundle(pathPrefix string, c2s, s2s, enableWebfinger bool, started time.Time, testRemoteActorID *url.URL) testBundle {
	tests := newCommonTests()
	if c2s {
		tests = append(tests, newSocialTests()...)
	}
	if s2s {
		tests = append(tests, newFederatingTests()...)
	}
	db := NewDatabase(ts.hostname)
	am := NewActorMapping()
	tsc := &TestServerClosure{
		ts,
		pathPrefix,
	}
	tr := NewTestRunner(tsc, tests)
	clock := &Clock{}
	handler := pub.NewActivityStreamsHandler(db, clock)
	actor := NewActor(db, am, tr, handler)
	pfa := pub.NewActor(actor, actor, actor, db, clock)
	ctx := &TestRunnerContext{
		TestRemoteActorID: testRemoteActorID,
		Actor:             pfa,
		DB:                db,
		AM:                am,
	}
	return testBundle{
		started:         started,
		enableWebfinger: enableWebfinger,
		tr:              tr,
		am:              am,
		pfa:             pfa,
		ctx:             ctx,
		db:              db,
		state: &testState{
			ID: testIdFromPathPrefix(pathPrefix),
		},
		handler: actor.PubHandlerFunc,
		stateMu: &sync.RWMutex{},
	}
}

const (
	kActor0          = "alex"
	kActor1          = "taylor"
	kActor2          = "logan"
	kActor3          = "austin"
	kActor4          = "peyton"
	kRecurCollection = "recursiveCollection"
)

func (ts *TestServer) prepareActor(c context.Context, tb testBundle, prefix, name string) (actor actorIDs, err error) {
	testID := testIdFromPathPrefix(prefix)
	actor = actorIDs{
		ActivityPubIRI: &url.URL{
			Scheme: "https",
			Host:   ts.hostname,
			Path:   path.Join(prefix, name),
		},
		WebfingerId:      fmt.Sprintf("@%s%s%s@%s", name, kWebfingerTestDelim, testID, ts.hostname),
		WebfingerSubject: fmt.Sprintf("%s%s%s", name, kWebfingerTestDelim, testID),
	}
	actorIRI := actor.ActivityPubIRI

	var kd KeyData
	kd, err = tb.am.generateKeyData(actorIRI)
	if err != nil {
		return
	}

	person := streams.NewActivityStreamsPerson()
	id := streams.NewJSONLDIdProperty()
	id.Set(actorIRI)
	person.SetJSONLDId(id)

	urlProp := streams.NewActivityStreamsUrlProperty()
	urlProp.AppendIRI(actorIRI)
	person.SetActivityStreamsUrl(urlProp)

	inboxIRI := ActorIRIToInboxIRI(actorIRI)
	inbox := streams.NewActivityStreamsInboxProperty()
	inbox.SetIRI(inboxIRI)
	person.SetActivityStreamsInbox(inbox)

	outboxIRI := ActorIRIToOutboxIRI(actorIRI)
	outbox := streams.NewActivityStreamsOutboxProperty()
	outbox.SetIRI(outboxIRI)
	person.SetActivityStreamsOutbox(outbox)

	followersIRI := ActorIRIToFollowersIRI(actorIRI)
	followers := streams.NewActivityStreamsFollowersProperty()
	followers.SetIRI(followersIRI)
	person.SetActivityStreamsFollowers(followers)

	followingIRI := ActorIRIToFollowingIRI(actorIRI)
	following := streams.NewActivityStreamsFollowingProperty()
	following.SetIRI(followingIRI)
	person.SetActivityStreamsFollowing(following)

	likedIRI := ActorIRIToLikedIRI(actorIRI)
	liked := streams.NewActivityStreamsLikedProperty()
	liked.SetIRI(likedIRI)
	person.SetActivityStreamsLiked(liked)

	nameProp := streams.NewActivityStreamsNameProperty()
	nameProp.AppendXMLSchemaString(name)
	person.SetActivityStreamsName(nameProp)

	preferredUsername := streams.NewActivityStreamsPreferredUsernameProperty()
	preferredUsername.SetXMLSchemaString(name)
	person.SetActivityStreamsPreferredUsername(preferredUsername)

	summary := streams.NewActivityStreamsSummaryProperty()
	summary.AppendXMLSchemaString("This is a test user, " + name)
	person.SetActivityStreamsSummary(summary)

	var pubPkix []byte
	pubPkix, err = x509.MarshalPKIXPublicKey(&kd.PrivKey.(*rsa.PrivateKey).PublicKey)
	if err != nil {
		return
	}
	pubBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubPkix,
	})
	pubString := string(pubBytes)

	pubk := streams.NewW3IDSecurityV1PublicKey()
	var pubkIRI *url.URL
	pubkIRI, err = url.Parse(kd.PubKeyURL)
	if err != nil {
		return
	}
	pubkID := streams.NewJSONLDIdProperty()
	pubkID.Set(pubkIRI)
	pubk.SetJSONLDId(pubkID)

	owner := streams.NewW3IDSecurityV1OwnerProperty()
	owner.SetIRI(actorIRI)
	pubk.SetW3IDSecurityV1Owner(owner)

	keyPem := streams.NewW3IDSecurityV1PublicKeyPemProperty()
	keyPem.Set(pubString)
	pubk.SetW3IDSecurityV1PublicKeyPem(keyPem)

	pubkProp := streams.NewW3IDSecurityV1PublicKeyProperty()
	pubkProp.AppendW3IDSecurityV1PublicKey(pubk)
	person.SetW3IDSecurityV1PublicKey(pubkProp)

	db := tb.db
	if err = db.Create(c, person); err != nil {
		return
	} else if err = createEmptyOrderedCollection(c, db, inboxIRI); err != nil {
		return
	} else if err = createEmptyOrderedCollection(c, db, outboxIRI); err != nil {
		return
	} else if err = createEmptyCollection(c, db, followersIRI); err != nil {
		return
	} else if err = createEmptyCollection(c, db, followingIRI); err != nil {
		return
	} else if err = createEmptyCollection(c, db, likedIRI); err != nil {
		return
	}
	return
}

func createEmptyOrderedCollection(c context.Context, db *Database, idIRI *url.URL) error {
	col := streams.NewActivityStreamsOrderedCollection()
	id := streams.NewJSONLDIdProperty()
	id.Set(idIRI)
	col.SetJSONLDId(id)

	return db.Create(c, col)
}

func createEmptyCollection(c context.Context, db *Database, idIRI *url.URL) error {
	col := streams.NewActivityStreamsCollection()
	id := streams.NewJSONLDIdProperty()
	id.Set(idIRI)
	col.SetJSONLDId(id)

	return db.Create(c, col)
}

/* ServerHandler */

var _ ServerHandler = &TestServerClosure{}

type TestServerClosure struct {
	*TestServer
	pathPrefix string
}

func (ts *TestServerClosure) Handle(i *Instruction) {
	ts.PathHandle(ts.pathPrefix, i)
}

func (ts *TestServerClosure) Update(pending, done []TestInfo, results []Result) {
	ts.PathUpdate(ts.pathPrefix, pending, done, results)
}

func (ts *TestServerClosure) Error(err error) {
	ts.PathError(ts.pathPrefix, err)
}

func (ts *TestServerClosure) MarkDone() {
	ts.PathMarkDone(ts.pathPrefix)
}

func (ts *TestServer) PathHandle(path string, i *Instruction) {
	ts.cacheMu.RLock()
	tb, ok := ts.cache[path]
	ts.cacheMu.RUnlock()
	if !ok {
		return
	}
	tb.stateMu.Lock()
	if i == nil {
		tb.state.I = Instruction{}
	} else {
		tb.state.I = *i
	}
	tb.stateMu.Unlock()
}

func (ts *TestServer) PathUpdate(path string, pending, done []TestInfo, results []Result) {
	ts.cacheMu.Lock()
	tb, ok := ts.cache[path]
	ts.cacheMu.Unlock()
	if !ok {
		return
	}
	tb.stateMu.Lock()
	tb.state.Pending = pending
	tb.state.Completed = done
	tb.state.Results = results
	tb.stateMu.Unlock()
}

func (ts *TestServer) PathError(path string, err error) {
	ts.cacheMu.RLock()
	tb, ok := ts.cache[path]
	ts.cacheMu.RUnlock()
	if !ok {
		return
	}
	tb.stateMu.Lock()
	tb.state.Err = err
	tb.stateMu.Unlock()
}

func (ts *TestServer) PathMarkDone(path string) {
	ts.cacheMu.RLock()
	tb, ok := ts.cache[path]
	ts.cacheMu.RUnlock()
	if !ok {
		return
	}
	tb.stateMu.Lock()
	tb.state.Done = true
	tb.stateMu.Unlock()
}
