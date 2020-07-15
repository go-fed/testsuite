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

type testBundle struct {
	started time.Time
	tr      *TestRunner
	am      *ActorMapping
	pfa     pub.FederatingActor
	ctx     *TestRunnerContext
	db      *Database
	handler pub.HandlerFunc
	state   *testState
	stateMu *sync.RWMutex
	timer   *time.Timer
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

func (ts *TestServer) StartTest(c context.Context, pathPrefix string, c2s, s2s bool, testRemoteActorID *url.URL) error {
	started := time.Now().UTC()
	tb := ts.newTestBundle(pathPrefix, c2s, s2s, started, testRemoteActorID)

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
	if IsRelativePathToInboxIRI(relPathPrefix) {
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
	} else if IsRelativePathToOutboxIRI(relPathPrefix) {
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

func (ts *TestServer) shutdown() {
	ts.cacheMu.Lock()
	for _, v := range ts.cache {
		v.timer.Stop()
		v.tr.Stop()
	}
	ts.cacheMu.Unlock()
}

func (ts *TestServer) newTestBundle(pathPrefix string, c2s, s2s bool, started time.Time, testRemoteActorID *url.URL) testBundle {
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
	actor := NewActor(db, am, tr)
	clock := &Clock{}
	pfa := pub.NewActor(actor, actor, actor, db, clock)
	ctx := &TestRunnerContext{
		TestRemoteActorID: testRemoteActorID,
		Actor:             pfa,
		DB:                db,
		AM:                am,
	}
	return testBundle{
		started: started,
		tr:      tr,
		am:      am,
		pfa:     pfa,
		ctx:     ctx,
		db:      db,
		state: &testState{
			ID: testIdFromPathPrefix(pathPrefix),
		},
		handler: pub.NewActivityStreamsHandler(db, clock),
		stateMu: &sync.RWMutex{},
	}
}

const (
	kActor0 = "alex"
	kActor1 = "taylor"
	kActor2 = "logan"
	kActor3 = "austin"
	kActor4 = "peyton"
)

func (ts *TestServer) prepareActor(c context.Context, tb testBundle, prefix, name string) (actorIRI *url.URL, err error) {
	actorIRI = &url.URL{
		Scheme: "https",
		Host:   ts.hostname,
		Path:   path.Join(prefix, name),
	}

	var kd KeyData
	kd, err = tb.am.generateKeyData(actorIRI)
	if err != nil {
		return
	}

	person := streams.NewActivityStreamsPerson()
	id := streams.NewJSONLDIdProperty()
	id.Set(actorIRI)
	person.SetJSONLDId(id)

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
	} else if err = createEmptyCollection(c, db, inboxIRI); err != nil {
		return
	} else if err = createEmptyCollection(c, db, outboxIRI); err != nil {
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
