package server

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync"

	"github.com/go-fed/activity/pub"
	"github.com/go-fed/activity/streams/vocab"
)

type ServerHandler interface {
	Handle(*Instruction)
	Update(pending, done []TestInfo, results []Result)
	Error(error)
	MarkDone()
}

type TestRunner struct {
	raw       *Recorder
	rawMu     sync.RWMutex
	tests     []Test
	completed []Test
	results   []Result
	sh        ServerHandler
	// Set once test is running
	cancel context.CancelFunc
	ctx    *TestRunnerContext
	// Flag bits used for synchronizing AP hook behaviors
	hookSyncMu                                    sync.Mutex
	awaitFederatedCoreActivity                    string
	awaitFederatedCoreActivityCreate              string
	awaitFederatedCoreActivityUpdate              string
	awaitFederatedCoreActivityDelete              string
	awaitFederatedCoreActivityFollow              string
	awaitFederatedCoreActivityAccept              string
	awaitFederatedCoreActivityReject              string
	awaitFederatedCoreActivityAdd                 string
	awaitFederatedCoreActivityRemove              string
	awaitFederatedCoreActivityLike                string
	awaitFederatedCoreActivityBlock               string
	awaitFederatedCoreActivityUndo                string
	awaitFederatedCoreActivityMaybeDoubleDelivery string
	httpSigsMustMatchRemoteActor                  bool
}

func NewTestRunner(sh ServerHandler, tests []Test) *TestRunner {
	return &TestRunner{
		tests:     tests,
		completed: make([]Test, 0, len(tests)),
		results:   make([]Result, 0, len(tests)),
		sh:        sh,
	}
}

func (tr *TestRunner) Run(ctx *TestRunnerContext) {
	if tr.cancel != nil {
		return
	}
	tr.ctx = ctx
	ctx.C, tr.cancel = context.WithCancel(context.Background())
	ctx.APH = tr
	go func() {
		defer func() {
			tr.sh.MarkDone()
		}()
		var err error
		for err == nil && len(tr.tests) > 0 {
			err = tr.iterate(ctx)
			if err != nil {
				tr.sh.Error(err)
				return
			} else {
				tc := make([]TestInfo, len(tr.tests))
				for i, t := range tr.tests {
					tc[i] = t.Info()
				}
				cc := make([]TestInfo, len(tr.completed))
				for i, t := range tr.completed {
					cc[i] = t.Info()
				}
				rc := make([]Result, len(tr.results))
				copy(rc, tr.results)
				tr.sh.Update(tc, cc, rc)
			}
			select {
			case <-ctx.C.Done():
				return
			default:
				// Nothing
			}
		}
	}()
}

func (tr *TestRunner) iterate(ctx *TestRunnerContext) error {
	var i *Instruction
	var r *Result
	var doneIdx int
	for idx, t := range tr.tests {
		tr.SetRecorder(t.Recorder())
		if i = t.MaybeGetInstructions(ctx, tr.results); i != nil {
			break
		} else if r = t.MaybeRunResult(ctx, tr.results); r != nil {
			doneIdx = idx
			break
		}
	}
	if i != nil {
		ctx.PrepInstructionResponse()
		tr.sh.Handle(i)
		select {
		case <-ctx.InstructionCh:
			// Remove any previous instructions
			tr.sh.Handle(nil)
			return nil
		case <-ctx.C.Done():
			return nil
		}
	}
	if r != nil {
		tr.results = append(tr.results, *r)
		tr.completed = append(tr.completed, tr.tests[doneIdx])
		copy(tr.tests[doneIdx:], tr.tests[doneIdx+1:])
		tr.tests[len(tr.tests)-1] = nil
		tr.tests = tr.tests[:len(tr.tests)-1]
		return nil
	}
	return fmt.Errorf("Neither an instruction nor result was obtained")
}

func (tr *TestRunner) Stop() {
	if tr.cancel == nil {
		return
	}
	tr.cancel()
	tr.cancel = nil
}

func (tr *TestRunner) ExpectFederatedCoreActivity(keyID string) {
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	tr.awaitFederatedCoreActivity = keyID
}

func (tr *TestRunner) ExpectFederatedCoreActivityHTTPSigsMustMatchTestRemoteActor(keyID string) {
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	tr.awaitFederatedCoreActivity = keyID
	tr.httpSigsMustMatchRemoteActor = true
}

func (tr *TestRunner) ExpectFederatedCoreActivityCreate(keyID string) {
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	tr.awaitFederatedCoreActivityCreate = keyID
}

func (tr *TestRunner) ExpectFederatedCoreActivityUpdate(keyID string) {
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	tr.awaitFederatedCoreActivityUpdate = keyID
}

func (tr *TestRunner) ExpectFederatedCoreActivityDelete(keyID string) {
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	tr.awaitFederatedCoreActivityDelete = keyID
}

func (tr *TestRunner) ExpectFederatedCoreActivityFollow(keyID string) {
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	tr.awaitFederatedCoreActivityFollow = keyID
}

func (tr *TestRunner) ExpectFederatedCoreActivityAccept(keyID string) {
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	tr.awaitFederatedCoreActivityAccept = keyID
}

func (tr *TestRunner) ExpectFederatedCoreActivityReject(keyID string) {
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	tr.awaitFederatedCoreActivityReject = keyID
}

func (tr *TestRunner) ExpectFederatedCoreActivityAdd(keyID string) {
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	tr.awaitFederatedCoreActivityAdd = keyID
}

func (tr *TestRunner) ExpectFederatedCoreActivityRemove(keyID string) {
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	tr.awaitFederatedCoreActivityRemove = keyID
}

func (tr *TestRunner) ExpectFederatedCoreActivityLike(keyID string) {
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	tr.awaitFederatedCoreActivityLike = keyID
}

func (tr *TestRunner) ExpectFederatedCoreActivityBlock(keyID string) {
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	tr.awaitFederatedCoreActivityBlock = keyID
}

func (tr *TestRunner) ExpectFederatedCoreActivityUndo(keyID string) {
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	tr.awaitFederatedCoreActivityUndo = keyID
}

func (tr *TestRunner) ExpectFederatedCoreActivityCheckDoubleDelivery(keyID string) {
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	tr.awaitFederatedCoreActivityMaybeDoubleDelivery = keyID
}

func (tr *TestRunner) ClearExpectations() {
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	tr.awaitFederatedCoreActivity = ""
	tr.awaitFederatedCoreActivityCreate = ""
	tr.awaitFederatedCoreActivityUpdate = ""
	tr.awaitFederatedCoreActivityDelete = ""
	tr.awaitFederatedCoreActivityFollow = ""
	tr.awaitFederatedCoreActivityAdd = ""
	tr.awaitFederatedCoreActivityRemove = ""
	tr.awaitFederatedCoreActivityLike = ""
	tr.awaitFederatedCoreActivityBlock = ""
	tr.awaitFederatedCoreActivityUndo = ""
	tr.awaitFederatedCoreActivityMaybeDoubleDelivery = ""
	tr.httpSigsMustMatchRemoteActor = false
}

func (tr *TestRunner) SetRecorder(r *Recorder) {
	tr.rawMu.Lock()
	defer tr.rawMu.Unlock()
	tr.raw = r
}

func (tr *TestRunner) Log(msg string, i ...interface{}) {
	tr.rawMu.RLock()
	defer tr.rawMu.RUnlock()
	if tr.raw == nil {
		return
	}
	tr.raw.Add(msg, i...)
}

func (tr *TestRunner) LogAuthenticateGetInbox(c context.Context, w http.ResponseWriter, r *http.Request, authenticated bool, err error) {
	tr.Log("LogAuthenticateGetInbox", c, w, r, authenticated, err)
}

func (tr *TestRunner) LogAuthenticateGetOutbox(c context.Context, w http.ResponseWriter, r *http.Request, authenticated bool, err error) {
	tr.Log("LogAuthenticateGetOutbox", c, w, r, authenticated, err)
}

func (tr *TestRunner) LogGetOutbox(c context.Context, r *http.Request, outboxId *url.URL, p vocab.ActivityStreamsOrderedCollectionPage, err error) {
	tr.Log("LogGetOutbox", c, r, outboxId, p, err)
}

func (tr *TestRunner) LogNewTransport(c context.Context, actorBoxIRI *url.URL, err error) {
	tr.Log("LogNewTransport", c, actorBoxIRI, err)
}

func (tr *TestRunner) LogDefaultCallback(c context.Context, activity pub.Activity) {
	tr.Log("LogDefaultCallback", c, activity)
}

func (tr *TestRunner) LogPostOutboxRequestBodyHook(c context.Context, r *http.Request, data vocab.Type) {
	tr.Log("LogPostOutboxRequestBodyHook", c, r, data)
}

func (tr *TestRunner) LogAuthenticatePostOutbox(c context.Context, w http.ResponseWriter, r *http.Request, authenticated bool, err error) {
	tr.Log("LogAuthenticatePostOutbox", c, w, r, authenticated, err)
}

func (tr *TestRunner) LogSocialCreate(c context.Context, v vocab.ActivityStreamsCreate) {
	tr.Log("LogSocialCreate", c, v)
}

func (tr *TestRunner) LogSocialUpdate(c context.Context, v vocab.ActivityStreamsUpdate) {
	tr.Log("LogSocialUpdate", c, v)
}

func (tr *TestRunner) LogSocialDelete(c context.Context, v vocab.ActivityStreamsDelete) {
	tr.Log("LogSocialDelete", c, v)
}

func (tr *TestRunner) LogSocialFollow(c context.Context, v vocab.ActivityStreamsFollow) {
	tr.Log("LogSocialFollow", c, v)
}

func (tr *TestRunner) LogSocialAdd(c context.Context, v vocab.ActivityStreamsAdd) {
	tr.Log("LogSocialAdd", c, v)
}

func (tr *TestRunner) LogSocialRemove(c context.Context, v vocab.ActivityStreamsRemove) {
	tr.Log("LogSocialRemove", c, v)
}

func (tr *TestRunner) LogSocialLike(c context.Context, v vocab.ActivityStreamsLike) {
	tr.Log("LogSocialLike", c, v)
}

func (tr *TestRunner) LogSocialUndo(c context.Context, v vocab.ActivityStreamsUndo) {
	tr.Log("LogSocialUndo", c, v)
}

func (tr *TestRunner) LogSocialBlock(c context.Context, v vocab.ActivityStreamsBlock) {
	tr.Log("LogSocialBlock", c, v)
}

func (tr *TestRunner) LogPostInboxRequestBodyHook(c context.Context, r *http.Request, activity pub.Activity) {
	tr.Log("LogPostInboxRequestBodyHook", c, r, activity)
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	if len(tr.awaitFederatedCoreActivityMaybeDoubleDelivery) > 0 {
		tr.Log("Checking double-delivery condition")
		iri, err := pub.GetId(activity)
		if err != nil {
			tr.Log("Error attempting to get the id of the activity", err)
			return
		}
		key := tr.awaitFederatedCoreActivityMaybeDoubleDelivery
		preIRI, err := getInstructionResponseAsDirectIRI(tr.ctx, key)
		if err != nil {
			tr.Log("First time seeing activity with id, because error returned", iri, err)
			tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		} else if preIRI.String() == iri.String() {
			tr.Log("Second time seeing activity with the same id", iri)
			tr.awaitFederatedCoreActivityMaybeDoubleDelivery = ""
			tr.ctx.C = context.WithValue(tr.ctx.C, key, []*url.URL{preIRI, iri})
		} else {
			tr.Log("Second time seeing activity, with different ids", iri, preIRI)
			tr.awaitFederatedCoreActivityMaybeDoubleDelivery = ""
			tr.ctx.C = context.WithValue(tr.ctx.C, key, []*url.URL{preIRI, iri})
		}
		tr.ctx.InstructionDone()
	}
}

func (tr *TestRunner) LogAuthenticatePostInbox(c context.Context, w http.ResponseWriter, r *http.Request, remoteActor *url.URL, authenticated bool, err error) {
	tr.Log("LogAuthenticatePostInbox", c, w, r, remoteActor, authenticated, err)
}

func (tr *TestRunner) LogBlocked(c context.Context, actorIRIs []*url.URL, blocked bool, err error) {
	tr.Log("LogBlocked", c, actorIRIs, blocked, err)
}

func (tr *TestRunner) LogFederatingCreate(c context.Context, v vocab.ActivityStreamsCreate) {
	tr.Log("LogFederatingCreate", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.Log("Could not get Create iri: " + err.Error())
		return
	}
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	if len(tr.awaitFederatedCoreActivity) > 0 {
		key := tr.awaitFederatedCoreActivity
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivity = ""
		tr.ctx.InstructionDone()
	}
	if len(tr.awaitFederatedCoreActivityCreate) > 0 {
		key := tr.awaitFederatedCoreActivityCreate
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivityCreate = ""
		tr.ctx.InstructionDone()
	}
}

func (tr *TestRunner) LogFederatingUpdate(c context.Context, v vocab.ActivityStreamsUpdate) {
	tr.Log("LogFederatingUpdate", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.Log("Could not get Update iri: " + err.Error())
		return
	}
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	if len(tr.awaitFederatedCoreActivity) > 0 {
		key := tr.awaitFederatedCoreActivity
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivity = ""
		tr.ctx.InstructionDone()
	}
	if len(tr.awaitFederatedCoreActivityUpdate) > 0 {
		key := tr.awaitFederatedCoreActivityUpdate
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivityUpdate = ""
		tr.ctx.InstructionDone()
	}
}

func (tr *TestRunner) LogFederatingDelete(c context.Context, v vocab.ActivityStreamsDelete) {
	tr.Log("LogFederatingDelete", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.Log("Could not get Delete iri: " + err.Error())
		return
	}
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	if len(tr.awaitFederatedCoreActivity) > 0 {
		key := tr.awaitFederatedCoreActivity
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivity = ""
		tr.ctx.InstructionDone()
	}
	if len(tr.awaitFederatedCoreActivityDelete) > 0 {
		key := tr.awaitFederatedCoreActivityDelete
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivityDelete = ""
		tr.ctx.InstructionDone()
	}
}

func (tr *TestRunner) LogFederatingFollow(c context.Context, v vocab.ActivityStreamsFollow) {
	tr.Log("LogFederatingFollow", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.Log("Could not get Follow iri: " + err.Error())
		return
	}
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	if len(tr.awaitFederatedCoreActivity) > 0 {
		key := tr.awaitFederatedCoreActivity
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivity = ""
		tr.ctx.InstructionDone()
	}
	if len(tr.awaitFederatedCoreActivityFollow) > 0 {
		key := tr.awaitFederatedCoreActivityFollow
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivityFollow = ""
		tr.ctx.InstructionDone()
	}
}

func (tr *TestRunner) LogFederatingAccept(c context.Context, v vocab.ActivityStreamsAccept) {
	tr.Log("LogFederatingAccept", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.Log("Could not get Accept iri: " + err.Error())
		return
	}
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	if len(tr.awaitFederatedCoreActivity) > 0 {
		key := tr.awaitFederatedCoreActivity
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivity = ""
		tr.ctx.InstructionDone()
	}
	if len(tr.awaitFederatedCoreActivityAccept) > 0 {
		key := tr.awaitFederatedCoreActivityAccept
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivityAccept = ""
		tr.ctx.InstructionDone()
	}
}

func (tr *TestRunner) LogFederatingReject(c context.Context, v vocab.ActivityStreamsReject) {
	tr.Log("LogFederatingReject", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.Log("Could not get Reject iri: " + err.Error())
		return
	}
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	if len(tr.awaitFederatedCoreActivity) > 0 {
		key := tr.awaitFederatedCoreActivity
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivity = ""
		tr.ctx.InstructionDone()
	}
	if len(tr.awaitFederatedCoreActivityReject) > 0 {
		key := tr.awaitFederatedCoreActivityReject
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivityReject = ""
		tr.ctx.InstructionDone()
	}
}

func (tr *TestRunner) LogFederatingAdd(c context.Context, v vocab.ActivityStreamsAdd) {
	tr.Log("LogFederatingAdd", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.Log("Could not get Add iri: " + err.Error())
		return
	}
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	if len(tr.awaitFederatedCoreActivity) > 0 {
		key := tr.awaitFederatedCoreActivity
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivity = ""
		tr.ctx.InstructionDone()
	}
	if len(tr.awaitFederatedCoreActivityAdd) > 0 {
		key := tr.awaitFederatedCoreActivityAdd
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivityAdd = ""
		tr.ctx.InstructionDone()
	}
}

func (tr *TestRunner) LogFederatingRemove(c context.Context, v vocab.ActivityStreamsRemove) {
	tr.Log("LogFederatingRemove", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.Log("Could not get Remove iri: " + err.Error())
		return
	}
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	if len(tr.awaitFederatedCoreActivity) > 0 {
		key := tr.awaitFederatedCoreActivity
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivity = ""
		tr.ctx.InstructionDone()
	}
	if len(tr.awaitFederatedCoreActivityRemove) > 0 {
		key := tr.awaitFederatedCoreActivityRemove
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivityRemove = ""
		tr.ctx.InstructionDone()
	}
}

func (tr *TestRunner) LogFederatingLike(c context.Context, v vocab.ActivityStreamsLike) {
	tr.Log("LogFederatingLike", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.Log("Could not get Like iri: " + err.Error())
		return
	}
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	if len(tr.awaitFederatedCoreActivity) > 0 {
		key := tr.awaitFederatedCoreActivity
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivity = ""
		tr.ctx.InstructionDone()
	}
	if len(tr.awaitFederatedCoreActivityLike) > 0 {
		key := tr.awaitFederatedCoreActivityLike
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivityLike = ""
		tr.ctx.InstructionDone()
	}
}

func (tr *TestRunner) LogFederatingUndo(c context.Context, v vocab.ActivityStreamsUndo) {
	tr.Log("LogFederatingUndo", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.Log("Could not get Undo iri: " + err.Error())
		return
	}
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	if len(tr.awaitFederatedCoreActivity) > 0 {
		key := tr.awaitFederatedCoreActivity
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivity = ""
		tr.ctx.InstructionDone()
	}
	if len(tr.awaitFederatedCoreActivityUndo) > 0 {
		key := tr.awaitFederatedCoreActivityUndo
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivityUndo = ""
		tr.ctx.InstructionDone()
	}
}

func (tr *TestRunner) LogFederatingBlock(c context.Context, v vocab.ActivityStreamsBlock) {
	tr.Log("LogFederatingBlock", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.Log("Could not get Block iri: " + err.Error())
		return
	}
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	if len(tr.awaitFederatedCoreActivity) > 0 {
		key := tr.awaitFederatedCoreActivity
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivity = ""
		tr.ctx.InstructionDone()
	}
	if len(tr.awaitFederatedCoreActivityBlock) > 0 {
		key := tr.awaitFederatedCoreActivityBlock
		tr.ctx.C = context.WithValue(tr.ctx.C, key, iri)
		tr.awaitFederatedCoreActivityBlock = ""
		tr.ctx.InstructionDone()
	}
}

func (tr *TestRunner) LogFilterForwarding(c context.Context, potentialRecipients []*url.URL, activity pub.Activity, filteredRecipients []*url.URL, err error) {
	tr.Log("LogFilterForwarding", c, potentialRecipients, activity, filteredRecipients, err)
}

func (tr *TestRunner) LogGetInbox(c context.Context, r *http.Request, outboxId *url.URL, p vocab.ActivityStreamsOrderedCollectionPage, err error) {
	tr.Log("LogGetInbox", c, r, outboxId, p, err)
}

func (tr *TestRunner) LogPubHandlerFuncAuthd(c context.Context, r *http.Request, isASRequest bool, err error, remoteActor *url.URL, authenticated bool, httpSigErr error) {
	tr.Log("LogHandle Web Request (with HTTP Signature)", c, r, isASRequest, err, remoteActor, authenticated, httpSigErr)
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	if tr.httpSigsMustMatchRemoteActor {
		matched := tr.ctx.TestRemoteActorID.String() == remoteActor.String()
		tr.ctx.C = context.WithValue(tr.ctx.C, kHttpSigMatchRemoteActorKeyId, matched)
		tr.httpSigsMustMatchRemoteActor = false
	}
	return
}
func (tr *TestRunner) LogPubHandlerFunc(c context.Context, r *http.Request, isASRequest bool, err, httpSigErr error) {
	tr.Log("LogHandle Web Request (no HTTP Signatures)", c, r, isASRequest, err, httpSigErr)
	tr.hookSyncMu.Lock()
	defer tr.hookSyncMu.Unlock()
	if tr.httpSigsMustMatchRemoteActor {
		tr.ctx.C = context.WithValue(tr.ctx.C, kHttpSigMatchRemoteActorKeyId, false)
		tr.httpSigsMustMatchRemoteActor = false
	}
	return
}
