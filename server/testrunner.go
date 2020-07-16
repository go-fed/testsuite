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
	tests     []Test
	completed []Test
	results   []Result
	sh        ServerHandler
	// Set once test is running
	cancel context.CancelFunc
	ctx    *TestRunnerContext
	// Flag bits used for synchronizing AP hook behaviors
	hookSyncMu                 sync.Mutex
	awaitFederatedCoreActivity string
}

func NewTestRunner(sh ServerHandler, tests []Test) *TestRunner {
	return &TestRunner{
		raw:       NewRecorder(),
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

func (tr *TestRunner) LogAuthenticateGetInbox(c context.Context, w http.ResponseWriter, r *http.Request, authenticated bool, err error) {
	tr.raw.Add("LogAuthenticateGetInbox", c, w, r, authenticated, err)
}

func (tr *TestRunner) LogAuthenticateGetOutbox(c context.Context, w http.ResponseWriter, r *http.Request, authenticated bool, err error) {
	tr.raw.Add("LogAuthenticateGetOutbox", c, w, r, authenticated, err)
}

func (tr *TestRunner) LogGetOutbox(c context.Context, r *http.Request, outboxId *url.URL, p vocab.ActivityStreamsOrderedCollectionPage, err error) {
	tr.raw.Add("LogGetOutbox", c, r, outboxId, p, err)
}

func (tr *TestRunner) LogNewTransport(c context.Context, actorBoxIRI *url.URL, err error) {
	tr.raw.Add("LogNewTransport", c, actorBoxIRI, err)
}

func (tr *TestRunner) LogDefaultCallback(c context.Context, activity pub.Activity) {
	tr.raw.Add("LogDefaultCallback", c, activity)
}

func (tr *TestRunner) LogPostOutboxRequestBodyHook(c context.Context, r *http.Request, data vocab.Type) {
	tr.raw.Add("LogPostOutboxRequestBodyHook", c, r, data)
}

func (tr *TestRunner) LogAuthenticatePostOutbox(c context.Context, w http.ResponseWriter, r *http.Request, authenticated bool, err error) {
	tr.raw.Add("LogAuthenticatePostOutbox", c, w, r, authenticated, err)
}

func (tr *TestRunner) LogSocialCreate(c context.Context, v vocab.ActivityStreamsCreate) {
	tr.raw.Add("LogSocialCreate", c, v)
}

func (tr *TestRunner) LogSocialUpdate(c context.Context, v vocab.ActivityStreamsUpdate) {
	tr.raw.Add("LogSocialUpdate", c, v)
}

func (tr *TestRunner) LogSocialDelete(c context.Context, v vocab.ActivityStreamsDelete) {
	tr.raw.Add("LogSocialDelete", c, v)
}

func (tr *TestRunner) LogSocialFollow(c context.Context, v vocab.ActivityStreamsFollow) {
	tr.raw.Add("LogSocialFollow", c, v)
}

func (tr *TestRunner) LogSocialAdd(c context.Context, v vocab.ActivityStreamsAdd) {
	tr.raw.Add("LogSocialAdd", c, v)
}

func (tr *TestRunner) LogSocialRemove(c context.Context, v vocab.ActivityStreamsRemove) {
	tr.raw.Add("LogSocialRemove", c, v)
}

func (tr *TestRunner) LogSocialLike(c context.Context, v vocab.ActivityStreamsLike) {
	tr.raw.Add("LogSocialLike", c, v)
}

func (tr *TestRunner) LogSocialUndo(c context.Context, v vocab.ActivityStreamsUndo) {
	tr.raw.Add("LogSocialUndo", c, v)
}

func (tr *TestRunner) LogSocialBlock(c context.Context, v vocab.ActivityStreamsBlock) {
	tr.raw.Add("LogSocialBlock", c, v)
}

func (tr *TestRunner) LogPostInboxRequestBodyHook(c context.Context, r *http.Request, activity pub.Activity) {
	tr.raw.Add("LogPostInboxRequestBodyHook", c, r, activity)
}

func (tr *TestRunner) LogAuthenticatePostInbox(c context.Context, w http.ResponseWriter, r *http.Request, authenticated bool, err error) {
	tr.raw.Add("LogAuthenticatePostInbox", c, w, r, authenticated, err)
}

func (tr *TestRunner) LogBlocked(c context.Context, actorIRIs []*url.URL, blocked bool, err error) {
	tr.raw.Add("LogBlocked", c, actorIRIs, blocked, err)
}

func (tr *TestRunner) LogFederatingCreate(c context.Context, v vocab.ActivityStreamsCreate) {
	tr.raw.Add("LogFederatingCreate", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.raw.Add("Could not get Create iri: " + err.Error())
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
}

func (tr *TestRunner) LogFederatingUpdate(c context.Context, v vocab.ActivityStreamsUpdate) {
	tr.raw.Add("LogFederatingUpdate", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.raw.Add("Could not get Update iri: " + err.Error())
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
}

func (tr *TestRunner) LogFederatingDelete(c context.Context, v vocab.ActivityStreamsDelete) {
	tr.raw.Add("LogFederatingDelete", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.raw.Add("Could not get Delete iri: " + err.Error())
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
}

func (tr *TestRunner) LogFederatingFollow(c context.Context, v vocab.ActivityStreamsFollow) {
	tr.raw.Add("LogFederatingFollow", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.raw.Add("Could not get Follow iri: " + err.Error())
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
}

func (tr *TestRunner) LogFederatingAdd(c context.Context, v vocab.ActivityStreamsAdd) {
	tr.raw.Add("LogFederatingAdd", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.raw.Add("Could not get Add iri: " + err.Error())
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
}

func (tr *TestRunner) LogFederatingRemove(c context.Context, v vocab.ActivityStreamsRemove) {
	tr.raw.Add("LogFederatingRemove", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.raw.Add("Could not get Remove iri: " + err.Error())
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
}

func (tr *TestRunner) LogFederatingLike(c context.Context, v vocab.ActivityStreamsLike) {
	tr.raw.Add("LogFederatingLike", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.raw.Add("Could not get Like iri: " + err.Error())
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
}

func (tr *TestRunner) LogFederatingUndo(c context.Context, v vocab.ActivityStreamsUndo) {
	tr.raw.Add("LogFederatingUndo", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.raw.Add("Could not get Undo iri: " + err.Error())
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
}

func (tr *TestRunner) LogFederatingBlock(c context.Context, v vocab.ActivityStreamsBlock) {
	tr.raw.Add("LogFederatingBlock", c, v)
	iri, err := pub.GetId(v)
	if err != nil {
		tr.raw.Add("Could not get Block iri: " + err.Error())
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
}

func (tr *TestRunner) LogFilterForwarding(c context.Context, potentialRecipients []*url.URL, activity pub.Activity, filteredRecipients []*url.URL, err error) {
	tr.raw.Add("LogFilterForwarding", c, potentialRecipients, activity, filteredRecipients, err)
}

func (tr *TestRunner) LogGetInbox(c context.Context, r *http.Request, outboxId *url.URL, p vocab.ActivityStreamsOrderedCollectionPage, err error) {
	tr.raw.Add("LogGetInbox", c, r, outboxId, p, err)
}
