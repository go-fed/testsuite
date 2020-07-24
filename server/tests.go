package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/go-fed/activity/pub"
	"github.com/go-fed/activity/streams"
	"github.com/go-fed/activity/streams/vocab"
)

type TestResultState string

const (
	TestResultNotRun       TestResultState = "Not Run"
	TestResultPass                         = "Pass"
	TestResultFail                         = "Fail"
	TestResultInconclusive                 = "Inconclusive"
)

type TestSpecKind string

const (
	TestSpecKindMust         TestSpecKind = "MUST"
	TestSpecKindShould       TestSpecKind = "SHOULD"
	TestSpecKindMay          TestSpecKind = "MAY"
	TestSpecKindNonNormative TestSpecKind = "NON-NORMATIVE"
)

type Result struct {
	TestName    string
	Description string
	State       TestResultState
	SpecKind    TestSpecKind
	Records     *Recorder
}

type instructionResponseType string

const (
	textBoxInstructionResponse    instructionResponseType = "text_box"
	checkBoxInstructionResponse                           = "checkbox"
	labelOnlyInstructionResponse                          = "label_only"
	doneButtonInstructionResponse                         = "done_button"
)

// Key IDs of user-submitted responses to instructions.
const (
	// Common Tests
	kSkipKeyIdSuffix                   = "_skip"
	kDereferenceIRIKeyId               = "instruction_key_dereference_iri"
	kTombstoneIRIKeyId                 = "instruction_key_tombstone_iri"
	kUsingTombstonesKeyId              = "instruction_key_using_tombstones"
	kNeverExistedIRIKeyId              = "instruction_key_never_existed_iri"
	kPrivateIRIKeyId                   = "instruction_key_private_object_iri"
	kDisclosesPrivateIRIExistenceKeyId = "instruction_key_discloses_private_object_existence"
	// Federated Tests
	kDeliveredFederatedActivity1KeyId   = "instruction_key_federated_activity_1"
	kFederatedOutboxIRIKeyID            = "auto_listen_key_federated_outbox_iri"
	kDeliveredFederatedActivityToKeyId  = "instruction_key_federated_activity_to"
	kDeliveredFederatedActivityCcKeyId  = "instruction_key_federated_activity_cc"
	kDeliveredFederatedActivityBtoKeyId = "instruction_key_federated_activity_bto"
	kDeliveredFederatedActivityBccKeyId = "instruction_key_federated_activity_bcc"
	kRecurrenceDeliveredActivityKeyId   = "instruction_key_recurrence_delivered_activity"
	kHttpSigMatchRemoteActorKeyId       = "test_runner_key_http_sig_remote_actor_match"
	kServerCreateActivityKeyId          = "instruction_key_federated_create_activity"
	kServerUpdateActivityKeyId          = "instruction_key_federated_update_activity"
	kServerDeleteActivityKeyId          = "instruction_key_federated_delete_activity"
	kServerFollowActivityKeyId          = "instruction_key_federated_follow_activity"
	kServerAddActivityKeyId             = "instruction_key_federated_add_activity"
	kServerRemoveActivityKeyId          = "instruction_key_federated_remove_activity"
	kServerLikeActivityKeyId            = "instruction_key_federated_like_activity"
	kServerBlockActivityKeyId           = "instruction_key_federated_block_activity"
	kServerBlockActivityDoneKeyId       = "instruction_key_federated_block_activity_done"
	kServerUndoActivityKeyId            = "instruction_key_federated_undo_activity"
	kServerDoubleDeliverActivityKeyId   = "instruction_key_federated_double_deliver_activity"
	kServerSelfDeliverActivityKeyId     = "instruction_key_federated_self_deliver_activity"
)

type instructionResponse struct {
	Key   string
	Type  instructionResponseType
	Label string
}

type Instruction struct {
	TestName     string
	Description  string
	Instructions string
	Skippable    bool
	Resp         []instructionResponse
}

type APHooks interface {
	ExpectFederatedCoreActivity(keyID string)
	ExpectFederatedCoreActivityHTTPSigsMustMatchTestRemoteActor(keyID string)
	ExpectFederatedCoreActivityCreate(keyID string)
	ExpectFederatedCoreActivityUpdate(keyID string)
	ExpectFederatedCoreActivityDelete(keyID string)
	ExpectFederatedCoreActivityFollow(keyID string)
	ExpectFederatedCoreActivityAdd(keyID string)
	ExpectFederatedCoreActivityRemove(keyID string)
	ExpectFederatedCoreActivityLike(keyID string)
	ExpectFederatedCoreActivityBlock(keyID string)
	ExpectFederatedCoreActivityUndo(keyID string)
	ExpectFederatedCoreActivityCheckDoubleDelivery(keyID string)
	ClearExpectations()
}

type actorIDs struct {
	ActivityPubIRI   *url.URL
	WebfingerId      string
	WebfingerSubject string
}

func (a actorIDs) String() string {
	s := a.ActivityPubIRI.String()
	if len(a.WebfingerId) > 0 {
		s = fmt.Sprintf("%s (or %s)", s, a.WebfingerId)
	}
	return s
}

type deliverableIDs struct {
	ActivityPubIRI   *url.URL
	WebfingerId      string
	WebfingerSubject string
}

func (d deliverableIDs) String() string {
	s := d.ActivityPubIRI.String()
	if len(d.WebfingerId) > 0 {
		s = fmt.Sprintf("%s (or %s)", s, d.WebfingerId)
	}
	return s
}

type TestRunnerContext struct {
	// Set by the server
	TestRemoteActorID     *url.URL
	Actor                 pub.FederatingActor
	DB                    *Database
	AM                    *ActorMapping
	TestActor0            actorIDs
	TestActor1            actorIDs
	TestActor2            actorIDs
	TestActor3            actorIDs
	TestActor4            actorIDs
	RecurLimitExceeded    bool           // S2S Only
	RootRecurCollectionID deliverableIDs // S2S Only
	// Set by the TestRunner
	C   context.Context
	APH APHooks
	// Read and Written by Tests
	InboxID *url.URL
	// Used for coordinating receiving instructions
	InstructionCh chan bool
}

func (trc *TestRunnerContext) PrepInstructionResponse() {
	trc.InstructionCh = make(chan bool)
}

func (trc *TestRunnerContext) InstructionDone() {
	if trc.InstructionCh != nil {
		close(trc.InstructionCh)
	}
}

type Test interface {
	MaybeGetInstructions(ctx *TestRunnerContext, existing []Result) *Instruction
	MaybeRunResult(ctx *TestRunnerContext, existing []Result) *Result
	Info() TestInfo
}

type TestInfo struct {
	TestName    string
	Description string
	State       TestResultState
	SpecKind    TestSpecKind
}

var _ Test = &baseTest{}

type baseTest struct {
	TestName               string
	Description            string
	ShouldSendInstructions func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction
	State                  TestResultState
	SpecKind               TestSpecKind
	R                      *Recorder
	Run                    func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool)
}

func (b *baseTest) Info() TestInfo {
	return TestInfo{
		TestName:    b.TestName,
		Description: b.Description,
		State:       b.State,
		SpecKind:    b.SpecKind,
	}
}

func (b *baseTest) MaybeGetInstructions(ctx *TestRunnerContext, existing []Result) *Instruction {
	if b.ShouldSendInstructions != nil {
		i := b.ShouldSendInstructions(b, ctx, existing)
		if i != nil {
			i.TestName = b.TestName
			i.Description = b.Description
			return i
		}
	}
	return nil
}

func (b *baseTest) MaybeRunResult(ctx *TestRunnerContext, existing []Result) *Result {
	if b.Run(b, ctx, existing) {
		return &Result{
			TestName:    b.TestName,
			Description: b.Description,
			State:       b.State,
			SpecKind:    b.SpecKind,
			Records:     b.R,
		}
	}
	return nil
}

func (b *baseTest) helperMustAddToDatabase(ctx *TestRunnerContext, iri *url.URL, t vocab.Type) bool {
	b.R.Add("Attempting to add the type to the database by IRI", iri, t.GetTypeName())
	err := ctx.DB.Lock(ctx.C, iri)
	if err != nil {
		b.R.Add("Internal error when locking Database", err)
		b.State = TestResultInconclusive
		return true
	}
	err = ctx.DB.Create(ctx.C, t)
	if err != nil {
		ctx.DB.Unlock(ctx.C, iri)
		b.R.Add("Internal error when calling Create in Database", err)
		b.State = TestResultInconclusive
		return true
	}
	err = ctx.DB.Unlock(ctx.C, iri)
	if err != nil {
		b.R.Add("Internal error when unlocking Database", err)
		b.State = TestResultInconclusive
		return true
	}
	b.R.Add("Successfully Create in database", iri)
	return false
}

func (b *baseTest) helperMustGetFromDatabase(ctx *TestRunnerContext, iri *url.URL) (done bool, t vocab.Type) {
	done = false
	b.R.Add("Attempting to fetch from Database by IRI", iri)
	err := ctx.DB.Lock(ctx.C, iri)
	if err != nil {
		b.R.Add("Internal error when locking Database", err)
		b.State = TestResultInconclusive
		done = true
		return
	}
	t, err = ctx.DB.Get(ctx.C, iri)
	if err != nil {
		ctx.DB.Unlock(ctx.C, iri)
		b.R.Add("Internal error when calling Get in Database", err)
		b.State = TestResultInconclusive
		done = true
		return
	}
	err = ctx.DB.Unlock(ctx.C, iri)
	if err != nil {
		b.R.Add("Internal error when unlocking Database", err)
		b.State = TestResultInconclusive
		done = true
		return
	}
	b.R.Add("Successfully fetched from database", iri)
	return
}

// TODO: Use an interface instead of *PlainTransport
func (b *baseTest) helperDereferenceWithCode(ctx *TestRunnerContext, iri *url.URL, tp *PlainTransport, expectCode int) (done bool, t vocab.Type) {
	done = false
	b.R.Add("Attempting to dereference the IRI", iri)
	by, code, err := tp.DereferenceWithStatusCode(ctx.C, iri)
	if err != nil {
		b.R.Add("Dereference had error", err)
		b.State = TestResultFail
		done = true
		return
	}
	b.R.Add("Dereference got bytes", len(by))
	b.R.Add("Dereference got code", code)
	if code != expectCode {
		b.R.Add(fmt.Sprintf("Expected http code %d but got %d", expectCode, code))
		b.State = TestResultFail
		done = true
		return
	}
	var m map[string]interface{}
	if err = json.Unmarshal(by, &m); err != nil {
		b.R.Add("Error when json.Unmarhsal", err)
		b.State = TestResultFail
		done = true
		return
	}
	t, err = streams.ToType(ctx.C, m)
	if err != nil {
		b.R.Add("Converting JSON to Golang type had error", err)
		b.State = TestResultFail
		done = true
		return
	}
	b.R.Add("Successfully called Dereference and converted to vocab.Type")
	return
}

func (b *baseTest) helperDereference(ctx *TestRunnerContext, iri *url.URL, tp pub.Transport) (done bool, t vocab.Type) {
	done = false
	b.R.Add("Attempting to dereference the IRI", iri)
	by, err := tp.Dereference(ctx.C, iri)
	if err != nil {
		b.R.Add("Dereference had error", err)
		b.State = TestResultFail
		done = true
		return
	}
	b.R.Add("Dereference got bytes", len(by))
	var m map[string]interface{}
	if err = json.Unmarshal(by, &m); err != nil {
		b.R.Add("Error when json.Unmarhsal", err)
		b.State = TestResultFail
		done = true
		return
	}
	t, err = streams.ToType(ctx.C, m)
	if err != nil {
		b.R.Add("Converting JSON to Golang type had error", err)
		b.State = TestResultFail
		done = true
		return
	}
	b.R.Add("Successfully called Dereference and converted to vocab.Type")
	return
}

func (b *baseTest) helperDereferenceNotFound(ctx *TestRunnerContext, iri *url.URL, tp *PlainTransport) (done bool) {
	done = false
	b.R.Add("Attempting to dereference the IRI", iri)
	_, code, err := tp.DereferenceWithStatusCode(ctx.C, iri)
	if err != nil {
		b.R.Add("Dereference had error", err)
		b.State = TestResultFail
		done = true
		return
	} else if code != http.StatusNotFound {
		b.R.Add(fmt.Sprintf("Dereference expected 404, got %d", code))
		b.State = TestResultFail
		done = true
		return
	}
	b.R.Add("Successfully called Dereference and got 404")
	return
}

func (b *baseTest) helperDereferenceForbidden(ctx *TestRunnerContext, iri *url.URL, tp *PlainTransport) (done bool) {
	done = false
	b.R.Add("Attempting to dereference the IRI", iri)
	_, code, err := tp.DereferenceWithStatusCode(ctx.C, iri)
	if err != nil {
		b.R.Add("Dereference had error", err)
		b.State = TestResultFail
		done = true
		return
	} else if code != http.StatusForbidden {
		b.R.Add(fmt.Sprintf("Dereference expected 403, got %d", code))
		b.State = TestResultFail
		done = true
		return
	}
	b.R.Add("Successfully called Dereference and got 403")
	return
}

type actorWrapper interface {
	GetActivityStreamsInbox() vocab.ActivityStreamsInboxProperty
	SetActivityStreamsInbox(i vocab.ActivityStreamsInboxProperty)
	GetActivityStreamsOutbox() vocab.ActivityStreamsOutboxProperty
	SetActivityStreamsOutbox(i vocab.ActivityStreamsOutboxProperty)
	GetActivityStreamsFollowing() vocab.ActivityStreamsFollowingProperty
	SetActivityStreamsFollowing(i vocab.ActivityStreamsFollowingProperty)
	GetActivityStreamsFollowers() vocab.ActivityStreamsFollowersProperty
	SetActivityStreamsFollowers(i vocab.ActivityStreamsFollowersProperty)
	GetActivityStreamsLiked() vocab.ActivityStreamsLikedProperty
	SetActivityStreamsLiked(i vocab.ActivityStreamsLikedProperty)
	SetActivityStreamsLikes(i vocab.ActivityStreamsLikesProperty)
	GetActivityStreamsLikes() vocab.ActivityStreamsLikesProperty
	GetActivityStreamsShares() vocab.ActivityStreamsSharesProperty
	SetActivityStreamsShares(i vocab.ActivityStreamsSharesProperty)
}

func (b *baseTest) helperToActor(ctx *TestRunnerContext, t vocab.Type) (done bool, a actorWrapper) {
	done = false
	tr, err := streams.NewTypeResolver(func(c context.Context, p vocab.ActivityStreamsPerson) error {
		a = p
		return nil
	}, func(c context.Context, app vocab.ActivityStreamsApplication) error {
		a = app
		return nil
	}, func(c context.Context, g vocab.ActivityStreamsGroup) error {
		a = g
		return nil
	}, func(c context.Context, o vocab.ActivityStreamsOrganization) error {
		a = o
		return nil
	}, func(c context.Context, s vocab.ActivityStreamsService) error {
		a = s
		return nil
	})
	if err != nil {
		b.R.Add("Internal error creating TypeResolver", err)
		b.State = TestResultInconclusive
		done = true
		return
	}
	if err = tr.Resolve(ctx.C, t); err != nil {
		b.R.Add("Error calling Resolve", err)
		b.State = TestResultFail
		done = true
		return
	}
	b.R.Add("Successfully converted vocab.Type to actorWrapper")
	return
}

type itemser interface {
	vocab.Type
	GetActivityStreamsOrderedItems() vocab.ActivityStreamsOrderedItemsProperty
}

func (b *baseTest) helperToOrderedCollectionOrPage(ctx *TestRunnerContext, t vocab.Type) (done bool, oc itemser) {
	done = false
	tr, err := streams.NewTypeResolver(func(c context.Context, ord vocab.ActivityStreamsOrderedCollection) error {
		oc = ord
		return nil
	}, func(c context.Context, ord vocab.ActivityStreamsOrderedCollectionPage) error {
		oc = ord
		return nil
	})
	if err != nil {
		b.R.Add("Internal error creating TypeResolver", err)
		b.State = TestResultInconclusive
		done = true
		return
	}
	if err = tr.Resolve(ctx.C, t); err != nil {
		b.R.Add("Error calling Resolve", err)
		b.State = TestResultFail
		done = true
		return
	}
	b.R.Add("Successfully converted vocab.Type to vocab.ActivityStreamsOrderedCollection or vocab.ActivityStreamsOrderedCollectionPage")
	return
}

func (b *baseTest) helperToTombstone(ctx *TestRunnerContext, t vocab.Type) (done bool, tomb vocab.ActivityStreamsTombstone) {
	done = false
	tr, err := streams.NewTypeResolver(func(c context.Context, tb vocab.ActivityStreamsTombstone) error {
		tomb = tb
		return nil
	})
	if err != nil {
		b.R.Add("Internal error creating TypeResolver", err)
		b.State = TestResultInconclusive
		done = true
		return
	}
	if err = tr.Resolve(ctx.C, t); err != nil {
		b.R.Add("Error calling Resolve", err)
		b.State = TestResultFail
		done = true
		return
	}
	b.R.Add("Successfully converted vocab.Type to vocab.ActivityStreamsTombstone")
	return
}

func (b *baseTest) helperOrderedItemsHasIRI(ctx *TestRunnerContext, toFind, examinedCollectionIRI *url.URL, oip vocab.ActivityStreamsOrderedItemsProperty) (done, found bool) {
	if oip != nil {
		for iter := oip.Begin(); iter != oip.End(); iter = iter.Next() {
			oiIRI, err := pub.ToId(iter)
			if err != nil {
				b.R.Add("Cannot get ID of orderedItems element", examinedCollectionIRI)
				b.State = TestResultFail
				done = true
				return
			}
			if oiIRI.String() == toFind.String() {
				found = true
				break
			}
		}
	}
	return
}

/* TEST HELPERS & CONSTANTS */

const (
	// Common Tests
	kGETActorTestName                                    = "GET Actor"
	kGETActorInboxTestName                               = "GET Actor Inbox"
	kActorInboxIsOrderedCollectionTestName               = "Actor Inbox Is OrderedCollection"
	kServerFiltersInboxBasedOnRequestersPermission       = "Server Filters Actor Inbox Based On Requester Permission"
	kServerAllowsDereferencingObjectIds                  = "Server Allows Dereferencing Object IDs"
	kServerAllowsDereferencingObjectIdsWithLDJSONProfile = "Server Allows Dereferencing Object IDs with ld+json and profile"
	kServerAllowsDereferencingObjectIdsWithActivityJSON  = "Server Allows Dereferencing Object IDs with activity+json "
	kServerTombstonesDeletedObjects                      = "Server Represents Deleted Objects With Tombstone"
	kServerDereferencesTombstoneWithGoneStatus           = "Server Responds With 410 Gone With Tombstone"
	kServerDereferencesNotFoundWhenNoTombstones          = "Server Responds With 404 Not Found When Not Using Tombstones"
	kServerObjectNeverExisted                            = "Server Handles When Object Never Existed"
	kServer404NotFoundNeverExisted                       = "Server Responds With 404 Not Found When Object Never Existed"
	kServerPrivateObjectIRI                              = "Server Handles Serving Access-Controlled Objects"
	kServerResponds403ForbiddenForPrivateObject          = "Server Responds With 403 Forbidden For Access-Controlled Objects"
	kServerResponds404NotFoundForPrivateObject           = "Server Responds With 404 Not Found For Access-Controlled Objects"
	// Federated Tests
	kServerDeliversOutboxActivities                 = "Delivers All Activities Posted In The Outbox"
	kGETActorOutboxTestName                         = "GET Actor Outbox"
	kServerDeliversActivityTo                       = "Uses `to` To Determine Delivery Recipients"
	kServerDeliversActivityCc                       = "Uses `cc` To Determine Delivery Recipients"
	kServerDeliversActivityBto                      = "Uses `bto` To Determine Delivery Recipients"
	kServerDeliversActivityBcc                      = "Uses `bcc` To Determine Delivery Recipients"
	kServerProvidesIdInNonTransientActivities       = "Provides An `id` In Non-Transient Activities Sent To Other Servers"
	kServerDereferencesWithUserCreds                = "Dereferences Delivery Targets With User's Credentials"
	kServerDeliversToActorsInCollections            = "Delivers To Actors In Collections/OrderedCollections"
	kServerDeliversToActorsInCollectionsRecursively = "Delivers To Nested Actors In Collections/OrderedCollections"
	kServerDeliversCreateWithObject                 = "Delivers Create With Object"
	kServerDeliversUpdateWithObject                 = "Delivers Update With Object"
	kServerDeliversDeleteWithObject                 = "Delivers Delete With Object"
	kServerDeliversFollowWithObject                 = "Delivers Follow With Object"
	kServerDeliversAddWithObjectAndTarget           = "Delivers Add With Object And Target"
	kServerDeliversRemoveWithObjectAndTarget        = "Delivers Remove With Object And Target"
	kServerDeliversLikeWithObject                   = "Delivers Like With Object"
	kServerDeliversBlockWithObject                  = "Delivers Block With Object"
	kServerDeliversUndoWithObject                   = "Delivers Undo With Object"
	kServerDoesNotDoubleDeliver                     = "Does Not Double-Deliver The Same Activity"
	kServerDoesNotSelfAddress                       = "Does Not Self-Address An Activity"
)

func getResultForTest(name string, existing []Result) *Result {
	for _, v := range existing {
		if v.TestName == name {
			return &v
		}
	}
	return nil
}

func hasTestPass(name string, existing []Result) bool {
	if r := getResultForTest(name, existing); r != nil {
		return r.State == TestResultPass
	}
	return false
}

func hasAnyRanResult(name string, existing []Result) bool {
	if r := getResultForTest(name, existing); r != nil {
		return r.State != TestResultNotRun
	}
	return false
}

func isInstructionResponseTrue(ctx *TestRunnerContext, keyID string) bool {
	s, err := getInstructionResponseAsOnlyString(ctx, keyID)
	return err == nil && s == "true"
}

func hasAnyInstructionKeys(ctx *TestRunnerContext, test string, keyIDs []string, skippable bool) bool {
	if skippable && hasSkippedTestName(ctx, test) {
		return true
	}
	for _, kid := range keyIDs {
		if ctx.C.Value(kid) != nil {
			return true
		}
	}
	return false
}

func hasAnyInstructionKey(ctx *TestRunnerContext, test string, keyID string, skippable bool) bool {
	return ctx.C.Value(keyID) != nil || (skippable && hasSkippedTestName(ctx, test))
}

func hasSkippedTestName(ctx *TestRunnerContext, test string) bool {
	return ctx.C.Value(test+kSkipKeyIdSuffix) != nil
}

func getInstructionResponseAsSliceOfIRIs(ctx *TestRunnerContext, keyID string) (iris []*url.URL, err error) {
	var ok bool
	iris, ok = ctx.C.Value(keyID).([]*url.URL)
	if !ok {
		err = fmt.Errorf("cannot get instruction key as []*url.URL: %s", keyID)
		return
	}
	return
}

func getInstructionResponseAsDirectIRI(ctx *TestRunnerContext, keyID string) (iri *url.URL, err error) {
	var ok bool
	iri, ok = ctx.C.Value(keyID).(*url.URL)
	if !ok {
		err = fmt.Errorf("cannot get instruction key as *url.URL: %s", keyID)
		return
	}
	return
}

func getInstructionResponseAsBool(ctx *TestRunnerContext, keyID string) (b bool, err error) {
	var ok bool
	b, ok = ctx.C.Value(keyID).(bool)
	if !ok {
		err = fmt.Errorf("cannot get instruction key as bool: %s", keyID)
		return
	}
	return
}

func getInstructionResponseAsOnlyIRI(ctx *TestRunnerContext, keyID string) (iri *url.URL, err error) {
	var s string
	s, err = getInstructionResponseAsOnlyString(ctx, keyID)
	if err != nil {
		return
	}
	iri, err = url.Parse(s)
	return
}

func getInstructionResponseAsOnlyString(ctx *TestRunnerContext, keyID string) (s string, err error) {
	strs, ok := ctx.C.Value(keyID).([]string)
	if !ok {
		err = fmt.Errorf("cannot get instruction key: %s", keyID)
		return
	}
	if len(strs) != 1 {
		err = fmt.Errorf("instruction key has more than one string value: %s", keyID)
		return
	}
	s = strs[0]
	return
}

/* COMMON SHARED TESTS */

func newCommonTests() []Test {
	return []Test{
		// GET Actor
		//
		// Side Effects:
		// - Adds remote actor to the Database
		&baseTest{
			TestName:    kGETActorTestName,
			Description: "Server responds to GET request at Actor URL",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				ptp := NewPlainTransport(me.R)
				me.R.Add(fmt.Sprintf("About to dereference actor at %s", ctx.TestRemoteActorID))
				done, t := me.helperDereference(ctx, ctx.TestRemoteActorID, ptp)
				if done {
					return true
				}
				id, err := pub.GetId(t)
				if err != nil {
					me.R.Add(fmt.Sprintf("Cannot get id of actor at %s", ctx.TestRemoteActorID))
				}
				ctx.TestRemoteActorID = id
				if me.helperMustAddToDatabase(ctx, id, t) {
					return true
				}
				me.State = TestResultPass
				return true
			},
		},

		// GET Actor Inbox
		//
		// Requires:
		// - Actor in the Database
		// Side Effects:
		// - Adds inbox to the Database
		// - Sets ctx.InboxID
		&baseTest{
			TestName:    kGETActorInboxTestName,
			Description: "Server responds to GET request at Actor's Inbox URL",
			SpecKind:    TestSpecKindNonNormative,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kGETActorTestName, existing) {
					return false
				} else if !hasTestPass(kGETActorTestName, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETActorTestName)
					me.State = TestResultInconclusive
					return true
				}
				done, at := me.helperMustGetFromDatabase(ctx, ctx.TestRemoteActorID)
				if done {
					return true
				}
				done, actor := me.helperToActor(ctx, at)
				if done {
					return true
				}
				inbox := actor.GetActivityStreamsInbox()
				if inbox == nil {
					me.R.Add("Actor at IRI does not have an inbox: ", ctx.TestRemoteActorID)
					me.State = TestResultFail
					return true
				}
				var err error
				ctx.InboxID, err = pub.ToId(inbox)
				if err != nil {
					me.R.Add("Could not determine the ID of the actor's inbox: ", err)
					me.State = TestResultFail
					return true
				}
				ptp := NewPlainTransport(me.R)
				me.R.Add(fmt.Sprintf("About to dereference inbox at %s", ctx.InboxID))
				done, t := me.helperDereference(ctx, ctx.InboxID, ptp)
				if done {
					return true
				}
				if me.helperMustAddToDatabase(ctx, ctx.InboxID, t) {
					return true
				}
				me.State = TestResultPass
				return true
			},
		},

		// Actor Inbox Is OrderedCollection
		//
		// Requires:
		// - inbox in the Database
		// - ctx.InboxID
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kActorInboxIsOrderedCollectionTestName,
			Description: "Inbox fetched from actor is OrderedCollection",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kGETActorInboxTestName, existing) {
					return false
				} else if !hasTestPass(kGETActorInboxTestName, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETActorInboxTestName)
					me.State = TestResultInconclusive
					return true
				}
				done, at := me.helperMustGetFromDatabase(ctx, ctx.InboxID)
				if done {
					return true
				}
				me.R.Add("Found inbox in database", at)
				done, oc := me.helperToOrderedCollectionOrPage(ctx, at)
				if done {
					return true
				}
				me.R.Add("Inbox is OrderedCollection or OrderedCollectionPage", oc)
				me.State = TestResultPass
				return true
			},
		},

		// Allows Dereferencing Object Ids
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - Populates the context with a value.
		&baseTest{
			TestName:    kServerAllowsDereferencingObjectIds,
			Description: "Allow dereferencing Object ids by responding to HTTP GET requests with a representation of the Object",
			SpecKind:    TestSpecKindMay,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerAllowsDereferencingObjectIds, kDereferenceIRIKeyId, skippable) {
					return &Instruction{
						Instructions: "Please enter an IRI of any publicly-accessible ActivityStreams content",
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:   kDereferenceIRIKeyId,
							Type:  textBoxInstructionResponse,
							Label: "IRI of public ActivityStreams content",
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerAllowsDereferencingObjectIds, kDereferenceIRIKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerAllowsDereferencingObjectIds) {
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsOnlyIRI(ctx, kDereferenceIRIKeyId)
				if err != nil {
					me.R.Add("Could not resolve the ID of the instruction: " + err.Error())
					me.State = TestResultFail
					return true
				}
				me.R.Add(fmt.Sprintf("Obtained IRI of the object at %s", iri))
				me.State = TestResultPass
				return true
			},
		},

		// Dereferences Object Id With ld+json plus profile
		//
		// Requires:
		// - An example ActivityStreams object IRI
		// Side Effects:
		// - Obtains an example ActivityStreams object IRI
		// - Stores it in the database
		&baseTest{
			TestName:    kServerAllowsDereferencingObjectIdsWithLDJSONProfile,
			Description: "Respond with the ActivityStreams object representation in response to requests that primarily `Accept` the media type `application/ld+json; profile=\"https://www.w3.org/ns/activitystreams\"`",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kServerAllowsDereferencingObjectIds, existing) {
					return false
				} else if !hasTestPass(kServerAllowsDereferencingObjectIds, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kServerAllowsDereferencingObjectIds)
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsOnlyIRI(ctx, kDereferenceIRIKeyId)
				if err != nil {
					me.R.Add("Could not resolve the ID of the instruction: " + err.Error())
					me.State = TestResultFail
					return true
				}
				ptp := NewPlainTransport(me.R)
				me.R.Add(fmt.Sprintf("About to dereference object at %s", iri))
				done, t := me.helperDereference(ctx, iri, ptp)
				if done {
					return true
				}
				if me.helperMustAddToDatabase(ctx, iri, t) {
					return true
				}
				me.State = TestResultPass
				return true
			},
		},

		// Dereferences Object Id With activity+json
		//
		// Requires:
		// - An example ActivityStreams object IRI
		// Side Effects:
		// - Obtains an example ActivityStreams object IRI
		// - Stores it in the database
		&baseTest{
			TestName:    kServerAllowsDereferencingObjectIdsWithActivityJSON,
			Description: "Respond with the ActivityStreams object representation in response to requests that primarily `Accept` the media type `application/activity+json`",
			SpecKind:    TestSpecKindShould,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kServerAllowsDereferencingObjectIds, existing) {
					return false
				} else if !hasTestPass(kServerAllowsDereferencingObjectIds, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kServerAllowsDereferencingObjectIds)
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsOnlyIRI(ctx, kDereferenceIRIKeyId)
				if err != nil {
					me.R.Add("Could not resolve the ID of the instruction: " + err.Error())
					me.State = TestResultFail
					return true
				}
				ptp := NewPlainTransportWithActivityJSON(me.R)
				me.R.Add(fmt.Sprintf("About to dereference object at %s", iri))
				done, t := me.helperDereference(ctx, iri, ptp)
				if done {
					return true
				}
				if me.helperMustAddToDatabase(ctx, iri, t) {
					return true
				}
				me.State = TestResultPass
				return true
			},
		},

		// Server Represents Deleted Objects With Tombstone
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - Populates the context with an deleted IRI value
		// - Populates the context with whether Tombstones are being used
		&baseTest{
			TestName:    kServerTombstonesDeletedObjects,
			Description: "If the server chooses to disclose that the object has been removed, responds with response body that is an ActivityStreams Object of type `Tombstone`",
			SpecKind:    TestSpecKindMay,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				const skippable = true
				if !hasAnyInstructionKeys(ctx, kServerTombstonesDeletedObjects, []string{kTombstoneIRIKeyId, kUsingTombstonesKeyId}, skippable) {
					return &Instruction{
						Instructions: "Please enter an IRI of a deleted or Tombstoned object, and indicate whether you're using Tombstones",
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:   kTombstoneIRIKeyId,
							Type:  textBoxInstructionResponse,
							Label: "IRI of deleted content",
						}, {
							Key:   kUsingTombstonesKeyId,
							Type:  checkBoxInstructionResponse,
							Label: "This deleted content is a Tombstone ActivityStreams object",
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				const skippable = true
				if !hasAnyInstructionKeys(ctx, kServerTombstonesDeletedObjects, []string{kTombstoneIRIKeyId, kUsingTombstonesKeyId}, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerTombstonesDeletedObjects) {
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsOnlyIRI(ctx, kTombstoneIRIKeyId)
				if err != nil {
					me.R.Add("Could not resolve the ID of the instruction: " + err.Error())
					me.State = TestResultFail
					return true
				}
				me.R.Add(fmt.Sprintf("Obtained IRI of the Tombstone object at %s", iri))
				me.State = TestResultPass
				return true
			},
		},

		// Server Responds With 410 Gone With Tombstone
		//
		// Requires:
		// - context has a deleted IRI value
		// - context has Tombstones enabled
		// Side Effects:
		// - Obtains a Tombstone ActivityStreams object IRI
		// - Stores it in the database
		&baseTest{
			TestName:    kServerDereferencesTombstoneWithGoneStatus,
			Description: "Respond with 410 Gone status code when Tombstone is in response body",
			SpecKind:    TestSpecKindShould,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kServerTombstonesDeletedObjects, existing) {
					return false
				} else if hasSkippedTestName(ctx, kServerTombstonesDeletedObjects) {
					me.R.Add("Skipping: skipped " + kServerTombstonesDeletedObjects)
					me.State = TestResultInconclusive
					return true
				} else if !isInstructionResponseTrue(ctx, kUsingTombstonesKeyId) {
					me.R.Add("Skipping: not using Tombstones")
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsOnlyIRI(ctx, kTombstoneIRIKeyId)
				if err != nil {
					me.R.Add("Could not resolve the ID of the instruction: " + err.Error())
					me.State = TestResultFail
					return true
				}
				ptp := NewPlainTransport(me.R)
				me.R.Add(fmt.Sprintf("About to dereference object at %s", iri))
				done, t := me.helperDereferenceWithCode(ctx, iri, ptp, http.StatusGone)
				if done {
					return true
				}
				done, tomb := me.helperToTombstone(ctx, t)
				if done {
					return true
				}
				me.R.Add("Object is Tombstone", tomb)
				if me.helperMustAddToDatabase(ctx, iri, tomb) {
					return true
				}
				me.State = TestResultPass
				return true
			},
		},

		// Server Responds With 404 Not Found When Not Using Tombstones
		//
		// Requires:
		// - context has a deleted IRI value
		// - context has Tombstones disabled
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerDereferencesNotFoundWhenNoTombstones,
			Description: "Respond with 404 Not Found status code for deleted objects when not using Tombstones",
			SpecKind:    TestSpecKindShould,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kServerTombstonesDeletedObjects, existing) {
					return false
				} else if hasSkippedTestName(ctx, kServerTombstonesDeletedObjects) {
					me.R.Add("Skipping: skipped " + kServerTombstonesDeletedObjects)
					me.State = TestResultInconclusive
					return true
				} else if isInstructionResponseTrue(ctx, kUsingTombstonesKeyId) {
					me.R.Add("Skipping: using Tombstones instead")
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsOnlyIRI(ctx, kTombstoneIRIKeyId)
				if err != nil {
					me.R.Add("Could not resolve the ID of the instruction: " + err.Error())
					me.State = TestResultFail
					return true
				}
				ptp := NewPlainTransport(me.R)
				me.R.Add(fmt.Sprintf("About to dereference object at %s", iri))
				done := me.helperDereferenceNotFound(ctx, iri, ptp)
				if done {
					return true
				}
				me.State = TestResultPass
				return true
			},
		},

		// Server Handles When Object Never Existed
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - Populates the context with a never-existed IRI value
		&baseTest{
			TestName:    kServerObjectNeverExisted,
			Description: "Responds for Object URIs that have never existed",
			SpecKind:    TestSpecKindShould,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerObjectNeverExisted, kNeverExistedIRIKeyId, skippable) {
					return &Instruction{
						Instructions: "Please enter an IRI of an object that has never existed",
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:   kNeverExistedIRIKeyId,
							Type:  textBoxInstructionResponse,
							Label: "IRI of content that has never existed",
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerObjectNeverExisted, kNeverExistedIRIKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerObjectNeverExisted) {
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsOnlyIRI(ctx, kNeverExistedIRIKeyId)
				if err != nil {
					me.R.Add("Could not resolve the ID of the instruction: " + err.Error())
					me.State = TestResultFail
					return true
				}
				me.R.Add(fmt.Sprintf("Obtained IRI of the object that has never existed at %s", iri))
				me.State = TestResultPass
				return true
			},
		},

		// Server Responds 404 Not Found For Objects That Never Existed
		//
		// Requires:
		// - Populates the context with a never-existed IRI value
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServer404NotFoundNeverExisted,
			Description: "Server responds with 404 status code for Object URIs that have never existed",
			SpecKind:    TestSpecKindShould,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kServerObjectNeverExisted, existing) {
					return false
				} else if !hasTestPass(kServerObjectNeverExisted, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kServerObjectNeverExisted)
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsOnlyIRI(ctx, kNeverExistedIRIKeyId)
				if err != nil {
					me.R.Add("Could not resolve the ID of the instruction: " + err.Error())
					me.State = TestResultFail
					return true
				}
				ptp := NewPlainTransport(me.R)
				me.R.Add(fmt.Sprintf("About to dereference never-existing object at %s", iri))
				done := me.helperDereferenceNotFound(ctx, iri, ptp)
				if done {
					return true
				}
				me.State = TestResultPass
				return true
			},
		},

		// Server Handles Serving Private IRI
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - Populates the context with a private IRI value
		// - Populates the context with whether server will acknowledge its existence
		&baseTest{
			TestName:    kServerPrivateObjectIRI,
			Description: "Responds for Object URIs that have never existed",
			SpecKind:    TestSpecKindShould,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				const skippable = true
				if !hasAnyInstructionKeys(ctx, kServerPrivateObjectIRI, []string{kPrivateIRIKeyId, kDisclosesPrivateIRIExistenceKeyId}, skippable) {
					return &Instruction{
						Instructions: "Please enter an IRI of an object that is private, and whether the server will acknowledge its existence when failing to properly access it",
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:   kPrivateIRIKeyId,
							Type:  textBoxInstructionResponse,
							Label: "IRI of content that is private",
						}, {
							Key:   kDisclosesPrivateIRIExistenceKeyId,
							Type:  checkBoxInstructionResponse,
							Label: "Server will respond with a 403 Forbidden response for unauthorized access",
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				const skippable = true
				if !hasAnyInstructionKeys(ctx, kServerPrivateObjectIRI, []string{kPrivateIRIKeyId, kDisclosesPrivateIRIExistenceKeyId}, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerPrivateObjectIRI) {
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsOnlyIRI(ctx, kPrivateIRIKeyId)
				if err != nil {
					me.R.Add("Could not resolve the ID of the instruction: " + err.Error())
					me.State = TestResultFail
					return true
				}
				me.R.Add(fmt.Sprintf("Obtained IRI of the private object at %s", iri))
				me.State = TestResultPass
				return true
			},
		},

		// Server Responds With 403 Forbidden For Access-Controlled Objects
		//
		// Requires:
		// - context has a private IRI value
		// - context has: server will acknowledge private objects' existence
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerResponds403ForbiddenForPrivateObject,
			Description: "Respond with 403 Forbidden status code when fetching access-controlled object",
			SpecKind:    TestSpecKindShould,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kServerPrivateObjectIRI, existing) {
					return false
				} else if hasSkippedTestName(ctx, kServerPrivateObjectIRI) {
					me.R.Add("Skipping: skipped " + kServerPrivateObjectIRI)
					me.State = TestResultInconclusive
					return true
				} else if !isInstructionResponseTrue(ctx, kDisclosesPrivateIRIExistenceKeyId) {
					me.R.Add("Skipping: server will not acknowledge existence")
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsOnlyIRI(ctx, kPrivateIRIKeyId)
				if err != nil {
					me.R.Add("Could not resolve the ID of the instruction: " + err.Error())
					me.State = TestResultFail
					return true
				}
				ptp := NewPlainTransport(me.R)
				me.R.Add(fmt.Sprintf("About to dereference object at %s", iri))
				done := me.helperDereferenceForbidden(ctx, iri, ptp)
				if done {
					return true
				}
				me.State = TestResultPass
				return true
			},
		},

		// Server Responds With 404 Not Found For Access-Controlled Objects
		//
		// Requires:
		// - context has a private IRI value
		// - context has: server will NOT acknowledge private objects' existence
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerResponds404NotFoundForPrivateObject,
			Description: "Respond with 404 Not Found status code when fetching access-controlled object",
			SpecKind:    TestSpecKindShould,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kServerPrivateObjectIRI, existing) {
					return false
				} else if hasSkippedTestName(ctx, kServerPrivateObjectIRI) {
					me.R.Add("Skipping: skipped " + kServerPrivateObjectIRI)
					me.State = TestResultInconclusive
					return true
				} else if isInstructionResponseTrue(ctx, kDisclosesPrivateIRIExistenceKeyId) {
					me.R.Add("Skipping: server will acknowledge existence")
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsOnlyIRI(ctx, kPrivateIRIKeyId)
				if err != nil {
					me.R.Add("Could not resolve the ID of the instruction: " + err.Error())
					me.State = TestResultFail
					return true
				}
				ptp := NewPlainTransport(me.R)
				me.R.Add(fmt.Sprintf("About to dereference object at %s", iri))
				done := me.helperDereferenceNotFound(ctx, iri, ptp)
				if done {
					return true
				}
				me.State = TestResultPass
				return true
			},
		},
	}
}

/* FEDERATING TESTS */

func newFederatingTests() []Test {
	return []Test{
		// Delivers All Activities Posted In The Outbox
		//
		// Requires:
		// - Remote actor in the Database
		// Side Effects:
		// - Populates the delivered activity in the context
		&baseTest{
			TestName:    kServerDeliversOutboxActivities,
			Description: "Performs delivery on all Activities posted to the outbox",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				if !hasAnyRanResult(kGETActorTestName, existing) || !hasTestPass(kGETActorTestName, existing) {
					return nil
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversOutboxActivities, kDeliveredFederatedActivity1KeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivity(kDeliveredFederatedActivity1KeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send an activity from %s to the test actor %s", ctx.TestRemoteActorID, ctx.TestActor0),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kDeliveredFederatedActivity1KeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kGETActorTestName, existing) {
					return false
				} else if !hasTestPass(kGETActorTestName, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETActorTestName)
					me.State = TestResultInconclusive
					return true
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversOutboxActivities, kDeliveredFederatedActivity1KeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDeliversOutboxActivities) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsDirectIRI(ctx, kDeliveredFederatedActivity1KeyId)
				if err != nil {
					me.R.Add("Could not resolve the ID of the activity: " + err.Error())
					me.State = TestResultFail
					return true
				}
				me.R.Add(fmt.Sprintf("Obtained the federated activity %s", iri))
				me.State = TestResultPass
				return true
			},
		},

		// GET Actor Outbox
		//
		// Requires:
		// - Actor in the Database
		// - Peer actor sent activity to us
		// Side Effects:
		// - Adds outbox to the Database
		// - Sets outbox ID onto the context
		&baseTest{
			TestName:    kGETActorOutboxTestName,
			Description: "Server responds to GET request at Actor's Outbox URL",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kGETActorTestName, existing) {
					return false
				} else if !hasAnyRanResult(kServerDeliversOutboxActivities, existing) {
					return false
				} else if !hasTestPass(kGETActorTestName, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETActorTestName)
					me.State = TestResultInconclusive
					return true
				} else if !hasTestPass(kServerDeliversOutboxActivities, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kServerDeliversOutboxActivities)
					me.State = TestResultInconclusive
					return true
				}
				done, at := me.helperMustGetFromDatabase(ctx, ctx.TestRemoteActorID)
				if done {
					return true
				}
				done, actor := me.helperToActor(ctx, at)
				if done {
					return true
				}
				outbox := actor.GetActivityStreamsOutbox()
				if outbox == nil {
					me.R.Add("Actor at IRI does not have an outbox: ", ctx.TestRemoteActorID)
					me.State = TestResultFail
					return true
				}
				outboxIRI, err := pub.ToId(outbox)
				if err != nil {
					me.R.Add("Could not determine the ID of the actor's outbox: ", err)
					me.State = TestResultFail
					return true
				}
				ctx.C = context.WithValue(ctx.C, kFederatedOutboxIRIKeyID, outboxIRI)
				ptp := NewPlainTransport(me.R)
				me.R.Add(fmt.Sprintf("About to dereference outbox at %s", outboxIRI))
				done, t := me.helperDereference(ctx, outboxIRI, ptp)
				if done {
					return true
				}
				if me.helperMustAddToDatabase(ctx, outboxIRI, t) {
					return true
				}
				me.State = TestResultPass
				return true
			},
		},

		// Delivers All Activities Posted In The Outbox
		//
		// Requires:
		// - Peer actor sent activity to us
		// - Outbox in the Database
		// - Outbox ID in the context
		// Side Effects:
		// - N/a
		&baseTest{
			TestName:    kServerDeliversOutboxActivities,
			Description: "Performs delivery on all Activities posted to the outbox",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				// Check dependencies
				if !hasAnyRanResult(kGETActorOutboxTestName, existing) {
					return false
				} else if !hasAnyRanResult(kServerDeliversOutboxActivities, existing) {
					return false
				} else if !hasTestPass(kGETActorOutboxTestName, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETActorOutboxTestName)
					me.State = TestResultInconclusive
					return true
				} else if !hasTestPass(kServerDeliversOutboxActivities, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kServerDeliversOutboxActivities)
					me.State = TestResultInconclusive
					return true
				}
				// Obtain the federated activity ID delivered to us
				iri, err := getInstructionResponseAsDirectIRI(ctx, kDeliveredFederatedActivity1KeyId)
				if err != nil {
					me.R.Add("Could not resolve the ID of the activity: " + err.Error())
					me.State = TestResultFail
					return true
				}
				// Now obtain the outbox that was fetched from the federated actor
				outboxIRI, err := getInstructionResponseAsDirectIRI(ctx, kFederatedOutboxIRIKeyID)
				if err != nil {
					me.R.Add("Could not resolve the ID of the outbox fetched: " + err.Error())
					me.State = TestResultFail
					return true
				}
				done, at := me.helperMustGetFromDatabase(ctx, outboxIRI)
				if done {
					return true
				}
				me.R.Add("Found outbox in database", outboxIRI)
				done, oc := me.helperToOrderedCollectionOrPage(ctx, at)
				if done {
					return true
				}
				orderedItemsProp := oc.GetActivityStreamsOrderedItems()
				done, found := me.helperOrderedItemsHasIRI(ctx, iri, outboxIRI, orderedItemsProp)
				if done {
					return true
				}
				if !found {
					me.R.Add("Could not find the activity ID in the outbox", iri, outboxIRI)
					me.State = TestResultFail
				} else {
					me.R.Add("Found the activity ID in the outbox", iri, outboxIRI)
					me.State = TestResultPass
				}
				return true
			},
		},

		// Uses `to` To Determine Delivery Recipients
		//
		// Requires:
		// - Remote actor in the Database
		// Side Effects:
		// - Populates the delivered activity in the context
		&baseTest{
			TestName:    kServerDeliversActivityTo,
			Description: "Utilizes the `to` field to determine delivery recipients",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				if !hasAnyRanResult(kGETActorTestName, existing) || !hasTestPass(kGETActorTestName, existing) {
					return nil
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversActivityTo, kDeliveredFederatedActivityToKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivity(kDeliveredFederatedActivityToKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send an activity from %s with the test actor in the `to` field of the activity: %s", ctx.TestRemoteActorID, ctx.TestActor0),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kDeliveredFederatedActivityToKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kGETActorTestName, existing) {
					return false
				} else if !hasTestPass(kGETActorTestName, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETActorTestName)
					me.State = TestResultInconclusive
					return true
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversActivityTo, kDeliveredFederatedActivityToKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDeliversActivityTo) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsDirectIRI(ctx, kDeliveredFederatedActivityToKeyId)
				if err != nil {
					me.R.Add("Could not obtain an activity: " + err.Error())
					me.State = TestResultFail
					return true
				}
				done, t := me.helperMustGetFromDatabase(ctx, iri)
				if done {
					return true
				}
				activity, ok := t.(pub.Activity)
				if !ok {
					me.R.Add("Could not resolve to the pub.Activity type", iri)
					me.State = TestResultFail
					return true
				}
				to := activity.GetActivityStreamsTo()
				if to == nil {
					me.R.Add("Activity has no `to` value", iri)
					me.State = TestResultFail
					return true
				}
				found := false
				for iter := to.Begin(); iter != to.End(); iter = iter.Next() {
					toIRI, err := pub.ToId(iter)
					if err != nil {
						me.R.Add("Could not convert a `to` value to an IRI")
						me.State = TestResultFail
						return true
					}
					if toIRI.String() == ctx.TestActor0.String() {
						found = true
						break
					}
				}
				if !found {
					me.R.Add("Could not find the actor in the `to` property of the activity", iri, ctx.TestActor0)
					me.State = TestResultFail
					return true
				}
				me.R.Add("Found the actor in the `to` property of the activity", ctx.TestActor0)
				me.State = TestResultPass
				return true
			},
		},

		// Uses `cc` To Determine Delivery Recipients
		//
		// Requires:
		// - Remote actor in the Database
		// Side Effects:
		// - Populates the delivered activity in the context
		&baseTest{
			TestName:    kServerDeliversActivityCc,
			Description: "Utilizes the `cc` field to determine delivery recipients",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				if !hasAnyRanResult(kGETActorTestName, existing) || !hasTestPass(kGETActorTestName, existing) {
					return nil
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversActivityCc, kDeliveredFederatedActivityCcKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivity(kDeliveredFederatedActivityCcKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send an activity from %s with the test actor in the `cc` field of the activity: %s", ctx.TestRemoteActorID, ctx.TestActor0),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kDeliveredFederatedActivityCcKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kGETActorTestName, existing) {
					return false
				} else if !hasTestPass(kGETActorTestName, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETActorTestName)
					me.State = TestResultInconclusive
					return true
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversActivityCc, kDeliveredFederatedActivityCcKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDeliversActivityCc) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsDirectIRI(ctx, kDeliveredFederatedActivityCcKeyId)
				if err != nil {
					me.R.Add("Could not obtain an activity: " + err.Error())
					me.State = TestResultFail
					return true
				}
				done, t := me.helperMustGetFromDatabase(ctx, iri)
				if done {
					return true
				}
				activity, ok := t.(pub.Activity)
				if !ok {
					me.R.Add("Could not resolve to the pub.Activity type", iri)
					me.State = TestResultFail
					return true
				}
				cc := activity.GetActivityStreamsCc()
				if cc == nil {
					me.R.Add("Activity has no `cc` value", iri)
					me.State = TestResultFail
					return true
				}
				found := false
				for iter := cc.Begin(); iter != cc.End(); iter = iter.Next() {
					ccIRI, err := pub.ToId(iter)
					if err != nil {
						me.R.Add("Could not convert a `cc` value to an IRI")
						me.State = TestResultFail
						return true
					}
					if ccIRI.String() == ctx.TestActor0.String() {
						found = true
						break
					}
				}
				if !found {
					me.R.Add("Could not find the actor in the `cc` property of the activity", iri, ctx.TestActor0)
					me.State = TestResultFail
					return true
				}
				me.R.Add("Found the actor in the `cc` property of the activity", ctx.TestActor0)
				me.State = TestResultPass
				return true
			},
		},

		// Uses `bto` To Determine Delivery Recipients
		//
		// Requires:
		// - Remote actor in the Database
		// Side Effects:
		// - Populates the delivered activity in the context
		&baseTest{
			TestName:    kServerDeliversActivityBto,
			Description: "Utilizes the `bto` field to determine delivery recipients",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				if !hasAnyRanResult(kGETActorTestName, existing) || !hasTestPass(kGETActorTestName, existing) {
					return nil
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversActivityBto, kDeliveredFederatedActivityBtoKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivity(kDeliveredFederatedActivityBtoKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send an activity from %s with the test actor in the `bto` field of the activity: %s", ctx.TestRemoteActorID, ctx.TestActor0),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kDeliveredFederatedActivityBtoKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kGETActorTestName, existing) {
					return false
				} else if !hasTestPass(kGETActorTestName, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETActorTestName)
					me.State = TestResultInconclusive
					return true
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversActivityBto, kDeliveredFederatedActivityBtoKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDeliversActivityBto) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsDirectIRI(ctx, kDeliveredFederatedActivityBtoKeyId)
				if err != nil {
					me.R.Add("Could not obtain an activity: " + err.Error())
					me.State = TestResultFail
					return true
				}
				done, t := me.helperMustGetFromDatabase(ctx, iri)
				if done {
					return true
				}
				activity, ok := t.(pub.Activity)
				if !ok {
					me.R.Add("Could not resolve to the pub.Activity type", iri)
					me.State = TestResultFail
					return true
				}
				bto := activity.GetActivityStreamsBto()
				if bto != nil && !bto.Empty() {
					me.R.Add("Activity has a `bto` value, which should be scrubbed before delivery")
					me.State = TestResultFail
					return true
				}
				me.R.Add("Found no actors in the `bto` property of the activity, this is expected and desired")
				me.State = TestResultPass
				return true
			},
		},

		// Uses `bcc` To Determine Delivery Recipients
		//
		// Requires:
		// - Remote actor in the Database
		// Side Effects:
		// - Populates the delivered activity in the context
		&baseTest{
			TestName:    kServerDeliversActivityBcc,
			Description: "Utilizes the `bcc` field to determine delivery recipients",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				if !hasAnyRanResult(kGETActorTestName, existing) || !hasTestPass(kGETActorTestName, existing) {
					return nil
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversActivityBcc, kDeliveredFederatedActivityBccKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivity(kDeliveredFederatedActivityBccKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send an activity from %s with the test actor in the `bcc` field of the activity: %s", ctx.TestRemoteActorID, ctx.TestActor0),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kDeliveredFederatedActivityBccKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kGETActorTestName, existing) {
					return false
				} else if !hasTestPass(kGETActorTestName, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETActorTestName)
					me.State = TestResultInconclusive
					return true
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversActivityBcc, kDeliveredFederatedActivityBccKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDeliversActivityBcc) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsDirectIRI(ctx, kDeliveredFederatedActivityBccKeyId)
				if err != nil {
					me.R.Add("Could not obtain an activity: " + err.Error())
					me.State = TestResultFail
					return true
				}
				done, t := me.helperMustGetFromDatabase(ctx, iri)
				if done {
					return true
				}
				activity, ok := t.(pub.Activity)
				if !ok {
					me.R.Add("Could not resolve to the pub.Activity type", iri)
					me.State = TestResultFail
					return true
				}
				bcc := activity.GetActivityStreamsBcc()
				if bcc != nil && !bcc.Empty() {
					me.R.Add("Activity has a `bcc` value, which should be scrubbed before delivery")
					me.State = TestResultFail
					return true
				}
				me.R.Add("Found no actors in the `bcc` property of the activity, this is expected and desired")
				me.State = TestResultPass
				return true
			},
		},

		// Provides An `id` In Non-Transient Activities Sent To Other Servers
		//
		// Requires:
		// - At least 1 of the delivery keys to be present
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerProvidesIdInNonTransientActivities,
			Description: "The `id` JSON-LD property is set on all federated activities received from peer",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kServerDeliversOutboxActivities, existing) ||
					!hasAnyRanResult(kServerDeliversActivityTo, existing) ||
					!hasAnyRanResult(kServerDeliversActivityCc, existing) ||
					!hasAnyRanResult(kServerDeliversActivityBto, existing) ||
					!hasAnyRanResult(kServerDeliversActivityBcc, existing) {
					return false
				}
				var keysToExamine []string
				if hasTestPass(kServerDeliversOutboxActivities, existing) {
					keysToExamine = append(keysToExamine, kDeliveredFederatedActivity1KeyId)
				}
				if hasTestPass(kServerDeliversActivityTo, existing) {
					keysToExamine = append(keysToExamine, kDeliveredFederatedActivityToKeyId)
				}
				if hasTestPass(kServerDeliversActivityCc, existing) {
					keysToExamine = append(keysToExamine, kDeliveredFederatedActivityCcKeyId)
				}
				if hasTestPass(kServerDeliversActivityBto, existing) {
					keysToExamine = append(keysToExamine, kDeliveredFederatedActivityBtoKeyId)
				}
				if hasTestPass(kServerDeliversActivityBcc, existing) {
					keysToExamine = append(keysToExamine, kDeliveredFederatedActivityBccKeyId)
				}
				if len(keysToExamine) == 0 {
					me.R.Add(fmt.Sprintf("Skipping: none of the dependency tests passed: [%s, %s, %s, %s, %s]",
						kServerDeliversOutboxActivities,
						kServerDeliversActivityTo,
						kServerDeliversActivityCc,
						kServerDeliversActivityBto,
						kServerDeliversActivityBcc))
					me.State = TestResultInconclusive
					return true
				}
				var irisToExamine []*url.URL
				for _, key := range keysToExamine {
					iri, err := getInstructionResponseAsDirectIRI(ctx, key)
					if err != nil {
						me.R.Add("Could not obtain an activity: " + err.Error())
						me.State = TestResultFail
						return true
					}
					irisToExamine = append(irisToExamine, iri)
				}
				for _, iri := range irisToExamine {
					done, t := me.helperMustGetFromDatabase(ctx, iri)
					if done {
						return true
					}
					_, err := pub.GetId(t)
					if err != nil {
						me.R.Add("Could not obtain the id off the activity", iri)
						me.State = TestResultFail
						return true
					}
				}
				me.R.Add("Verified all activities have `id` set", irisToExamine)
				me.State = TestResultPass
				return true
			},
		},

		// Dereferences Delivery Targets With User's Credentials
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - Sets the recursive-testing activity in the context
		&baseTest{
			TestName:    kServerDereferencesWithUserCreds,
			Description: "Dereferences delivery targets with the submitting user's credentials",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDereferencesWithUserCreds, kRecurrenceDeliveredActivityKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivityHTTPSigsMustMatchTestRemoteActor(kRecurrenceDeliveredActivityKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send an activity from %s to the collection: %s", ctx.TestRemoteActorID, ctx.RootRecurCollectionID),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kRecurrenceDeliveredActivityKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDereferencesWithUserCreds, kRecurrenceDeliveredActivityKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDereferencesWithUserCreds) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				matched, err := getInstructionResponseAsBool(ctx, kHttpSigMatchRemoteActorKeyId)
				if err != nil {
					me.R.Add("Could not obtain whether httpsigs matched: " + err.Error())
					me.State = TestResultFail
					return true
				}
				if !matched {
					me.R.Add("Did not dereference with the user's credentials")
					me.State = TestResultFail
					return true
				}
				iri, err := getInstructionResponseAsDirectIRI(ctx, kRecurrenceDeliveredActivityKeyId)
				if err != nil {
					me.R.Add("Could not obtain an activity: " + err.Error())
					me.State = TestResultFail
					return true
				}
				me.R.Add("Successfully used remote actor's credentials to dereference the collection, and then successfully received an activity", iri)
				me.State = TestResultPass
				return true
			},
		},

		// Delivers To Actors In Collections/OrderedCollections
		//
		// Requires:
		// - Sets the recursive-testing activity in the context
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerDeliversToActorsInCollections,
			Description: "Delivers to all items in recipients that are Collections or OrderedCollections",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kServerDereferencesWithUserCreds, existing) {
					return false
				} else if !hasTestPass(kServerDereferencesWithUserCreds, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kServerDereferencesWithUserCreds)
					me.State = TestResultInconclusive
					return true
				}
				// Get IRI of the activity received
				iri, err := getInstructionResponseAsDirectIRI(ctx, kRecurrenceDeliveredActivityKeyId)
				if err != nil {
					me.R.Add("Could not obtain an activity: " + err.Error())
					me.State = TestResultFail
					return true
				}
				// Get kActor1 Inbox
				actor1InboxIRI := ActorIRIToInboxIRI(ctx.TestActor1.ActivityPubIRI)
				me.R.Add("Getting inbox", actor1InboxIRI)
				done, t1 := me.helperMustGetFromDatabase(ctx, actor1InboxIRI)
				if done {
					return true
				}
				done, oc1 := me.helperToOrderedCollectionOrPage(ctx, t1)
				if done {
					return true
				}
				// Get kActor2 Inbox
				actor2InboxIRI := ActorIRIToInboxIRI(ctx.TestActor2.ActivityPubIRI)
				me.R.Add("Getting inbox", actor2InboxIRI)
				done, t2 := me.helperMustGetFromDatabase(ctx, actor2InboxIRI)
				if done {
					return true
				}
				done, oc2 := me.helperToOrderedCollectionOrPage(ctx, t2)
				if done {
					return true
				}
				// Ensure both their inboxes have the IRI of the activity.
				orderedItems1Prop := oc1.GetActivityStreamsOrderedItems()
				done, found1 := me.helperOrderedItemsHasIRI(ctx, iri, actor1InboxIRI, orderedItems1Prop)
				if done {
					return true
				}
				orderedItems2Prop := oc2.GetActivityStreamsOrderedItems()
				done, found2 := me.helperOrderedItemsHasIRI(ctx, iri, actor2InboxIRI, orderedItems2Prop)
				if done {
					return true
				}
				if !found1 && !found2 {
					me.R.Add("Could not find the activity ID in any of the inboxes", iri, actor1InboxIRI, actor2InboxIRI)
					me.State = TestResultFail
				} else if !found1 {
					me.R.Add("Could not find the activity ID in one of the two inboxes", iri, actor1InboxIRI)
					me.State = TestResultFail
				} else if !found2 {
					me.R.Add("Could not find the activity ID in one of the two inboxes", iri, actor2InboxIRI)
					me.State = TestResultFail
				} else {
					me.R.Add("Found the activity ID in both inboxes", iri, actor1InboxIRI, actor2InboxIRI)
					me.State = TestResultPass
				}
				return true
			},
		},

		// Delivers To Nested Actors In Collections/OrderedCollections
		//
		// Requires:
		// - Sets the recursive-testing activity in the context
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerDeliversToActorsInCollectionsRecursively,
			Description: "Delivers to nested actors in Collections recursively if the Collection contains Collections, and limits recursion depth >= 1",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kServerDereferencesWithUserCreds, existing) {
					return false
				} else if !hasTestPass(kServerDereferencesWithUserCreds, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kServerDereferencesWithUserCreds)
					me.State = TestResultInconclusive
					return true
				}
				// Get IRI of the activity received
				iri, err := getInstructionResponseAsDirectIRI(ctx, kRecurrenceDeliveredActivityKeyId)
				if err != nil {
					me.R.Add("Could not obtain an activity: " + err.Error())
					me.State = TestResultFail
					return true
				}
				// Get kActor3 Inbox
				actor3InboxIRI := ActorIRIToInboxIRI(ctx.TestActor3.ActivityPubIRI)
				me.R.Add("Getting inbox", actor3InboxIRI)
				done, t3 := me.helperMustGetFromDatabase(ctx, actor3InboxIRI)
				if done {
					return true
				}
				done, oc3 := me.helperToOrderedCollectionOrPage(ctx, t3)
				if done {
					return true
				}
				// Get kActor4 Inbox
				actor4InboxIRI := ActorIRIToInboxIRI(ctx.TestActor4.ActivityPubIRI)
				me.R.Add("Getting inbox", actor4InboxIRI)
				done, t4 := me.helperMustGetFromDatabase(ctx, actor4InboxIRI)
				if done {
					return true
				}
				done, oc4 := me.helperToOrderedCollectionOrPage(ctx, t4)
				if done {
					return true
				}
				// Determine whether kActor3 and kActor4 have the activity in their inbox
				orderedItems3Prop := oc3.GetActivityStreamsOrderedItems()
				done, found3 := me.helperOrderedItemsHasIRI(ctx, iri, actor3InboxIRI, orderedItems3Prop)
				if done {
					return true
				}
				orderedItems4Prop := oc4.GetActivityStreamsOrderedItems()
				done, found4 := me.helperOrderedItemsHasIRI(ctx, iri, actor4InboxIRI, orderedItems4Prop)
				if done {
					return true
				}
				if found3 && !found4 {
					// Passing case, but RecurLimitExceeded may have happened during setup
					if ctx.RecurLimitExceeded {
						me.R.Add("Found the activity ID in one inbox and not the too-nested-deep inbox, as expected", iri, actor3InboxIRI, actor4InboxIRI)
						me.State = TestResultPass
					} else {
						me.R.Add("Found the activity ID in one inbox and not the too-nested-deep inbox, as expected. However, during test setup the test server hit its own limit, meaning there is a mismatch between testing parameters and effective behavior!", iri, actor3InboxIRI, actor4InboxIRI)
						me.State = TestResultFail
					}
				} else {
					// Failing case, but RecurLimitExceeded may have happened during setup
					if found3 && found4 && ctx.RecurLimitExceeded {
						me.R.Add("Found the activity ID in both inboxes, which is not desired, but during test setup this test server hit its own recursion limit. The software under test recurs deeper than this test server is permitted. So this test cannot determine whether the federated peer passes nor fails!", iri, actor3InboxIRI, actor4InboxIRI)
						me.State = TestResultInconclusive
					} else if !found3 {
						me.R.Add("The peer did not recur far enough to deliver to the inbox", iri, actor3InboxIRI)
						me.State = TestResultFail
					} else if found4 && !ctx.RecurLimitExceeded {
						me.R.Add("The peer recursively went too far to deliver to the inbox", iri, actor4InboxIRI)
						me.State = TestResultFail
					} else {
						me.R.Add("Did not find the activity in one inbox, and not in the too-deep inbox, without hitting the test server's own recursive limitations.", iri, actor3InboxIRI, actor4InboxIRI)
						me.State = TestResultFail
					}
				}
				return true
			},
		},

		// Delivers Create With Object
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerDeliversCreateWithObject,
			Description: "Delivers activity with 'object' property if the Activity type is a Create",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversCreateWithObject, kServerCreateActivityKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivityCreate(kServerCreateActivityKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send a Create activity from %s to the test actor %s", ctx.TestRemoteActorID, ctx.TestActor0),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kServerCreateActivityKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversCreateWithObject, kServerCreateActivityKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDeliversCreateWithObject) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				me.R.Add("Callback was successfully called after examining the activity.")
				me.State = TestResultPass
				return true
			},
		},

		// Delivers Update With Object
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerDeliversUpdateWithObject,
			Description: "Delivers activity with 'object' property if the Activity type is an Update",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversUpdateWithObject, kServerUpdateActivityKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivityUpdate(kServerUpdateActivityKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send a Update activity from %s to the test actor %s", ctx.TestRemoteActorID, ctx.TestActor0),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kServerUpdateActivityKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversUpdateWithObject, kServerUpdateActivityKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDeliversUpdateWithObject) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				me.R.Add("Callback was successfully called after examining the activity.")
				me.State = TestResultPass
				return true
			},
		},

		// Delivers Delete With Object
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerDeliversDeleteWithObject,
			Description: "Delivers activity with 'object' property if the Activity type is a Delete",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversDeleteWithObject, kServerDeleteActivityKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivityDelete(kServerDeleteActivityKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send a Delete activity from %s to the test actor %s", ctx.TestRemoteActorID, ctx.TestActor0),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kServerDeleteActivityKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversDeleteWithObject, kServerDeleteActivityKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDeliversDeleteWithObject) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				me.R.Add("Callback was successfully called after examining the activity.")
				me.State = TestResultPass
				return true
			},
		},

		// Delivers Follow With Object
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerDeliversFollowWithObject,
			Description: "Delivers activity with 'object' property if the Activity type is a Follow",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversFollowWithObject, kServerFollowActivityKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivityFollow(kServerFollowActivityKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send a Follow activity from %s to the test actor %s", ctx.TestRemoteActorID, ctx.TestActor0),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kServerFollowActivityKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversFollowWithObject, kServerFollowActivityKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDeliversFollowWithObject) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				me.R.Add("Callback was successfully called after examining the activity.")
				me.State = TestResultPass
				return true
			},
		},

		// Delivers Add With Object And Target
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerDeliversAddWithObjectAndTarget,
			Description: "Delivers activity with 'object' and 'target' properties if the Activity type is an Add",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversAddWithObjectAndTarget, kServerAddActivityKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivityAdd(kServerAddActivityKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send an Add activity from %s to the test actor %s", ctx.TestRemoteActorID, ctx.TestActor0),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kServerAddActivityKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversAddWithObjectAndTarget, kServerAddActivityKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDeliversAddWithObjectAndTarget) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				me.R.Add("Callback was successfully called after examining the activity.")
				me.State = TestResultPass
				return true
			},
		},

		// Delivers Remove With Object And Target
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerDeliversRemoveWithObjectAndTarget,
			Description: "Delivers activity with 'object' and 'target' properties if the Activity type is a Remove",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversRemoveWithObjectAndTarget, kServerRemoveActivityKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivityRemove(kServerRemoveActivityKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send a Remove activity from %s to the test actor %s", ctx.TestRemoteActorID, ctx.TestActor0),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kServerRemoveActivityKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversRemoveWithObjectAndTarget, kServerRemoveActivityKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDeliversRemoveWithObjectAndTarget) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				me.R.Add("Callback was successfully called after examining the activity.")
				me.State = TestResultPass
				return true
			},
		},

		// Delivers Like With Object
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerDeliversLikeWithObject,
			Description: "Delivers activity with 'object' property if the Activity type is a Like",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversLikeWithObject, kServerLikeActivityKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivityLike(kServerLikeActivityKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send a Like activity from %s to the test actor %s", ctx.TestRemoteActorID, ctx.TestActor0),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kServerLikeActivityKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversLikeWithObject, kServerLikeActivityKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDeliversLikeWithObject) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				me.R.Add("Callback was successfully called after examining the activity.")
				me.State = TestResultPass
				return true
			},
		},

		// Delivers Block With Object
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerDeliversBlockWithObject,
			Description: "Delivers activity with 'object' property if the Activity type is a Block",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversBlockWithObject, kServerBlockActivityKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivityBlock(kServerBlockActivityKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please perform a Block activity from %s to the test actor %s. Once done, if you do not send a Block activity to federated peers, click 'Done' below", ctx.TestRemoteActorID, ctx.TestActor0),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kServerBlockActivityKeyId,
							Type: labelOnlyInstructionResponse,
						}, {
							Key:  kServerBlockActivityDoneKeyId,
							Type: doneButtonInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversBlockWithObject, kServerBlockActivityKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDeliversBlockWithObject) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				me.R.Add("Callback was successfully called after examining the activity.")
				me.State = TestResultPass
				return true
			},
		},

		// Delivers Undo With Object
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerDeliversUndoWithObject,
			Description: "Delivers activity with 'object' property if the Activity type is an Undo",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversUndoWithObject, kServerUndoActivityKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivityUndo(kServerUndoActivityKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please perform an Undo activity from %s to the test actor %s", ctx.TestRemoteActorID, ctx.TestActor0),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kServerUndoActivityKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDeliversUndoWithObject, kServerUndoActivityKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDeliversUndoWithObject) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				me.R.Add("Callback was successfully called after examining the activity.")
				me.State = TestResultPass
				return true
			},
		},

		// Does Not Double-Deliver The Same Activity
		//
		// Requires:
		// - Remote actor in the Database
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerDoesNotDoubleDeliver,
			Description: "Deduplicates final recipient list",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				if !hasAnyRanResult(kGETActorTestName, existing) || !hasTestPass(kGETActorTestName, existing) {
					return nil
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDoesNotDoubleDeliver, kServerDoubleDeliverActivityKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivityCheckDoubleDelivery(kServerDoubleDeliverActivityKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send an activity from %s to both %s and %s (the same actor twice)", ctx.TestRemoteActorID, ctx.TestActor0, ctx.TestActor0),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kServerDoubleDeliverActivityKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kGETActorTestName, existing) {
					return false
				} else if !hasTestPass(kGETActorTestName, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETActorTestName)
					me.State = TestResultInconclusive
					return true
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDoesNotDoubleDeliver, kServerDoubleDeliverActivityKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDoesNotDoubleDeliver) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				// We don't have a good way to await for the possibility of the
				// second activity to maybe come. So simply sleep an additional
				// five seconds
				me.R.Add("Sleep five seconds to await possibility of double-delivery")
				time.Sleep(5 * time.Second)
				// Must be called to clear overwrite condition
				ctx.APH.ClearExpectations()
				var iris []*url.URL
				iri, err := getInstructionResponseAsDirectIRI(ctx, kServerDoubleDeliverActivityKeyId)
				if err != nil {
					iris, err = getInstructionResponseAsSliceOfIRIs(ctx, kServerDoubleDeliverActivityKeyId)
					if err != nil {
						me.R.Add("Could not iri(s) for double-detection test: " + err.Error())
						me.State = TestResultFail
						return true
					}
				}
				if len(iris) > 1 {
					me.R.Add("Same activity received more than once.", len(iris))
					me.State = TestResultFail
				} else if iri != nil {
					me.R.Add("Got the activity only once", iri)
					me.State = TestResultPass
				} else if len(iris) == 1 {
					me.R.Add("Got the activity only once", iris[0])
					me.State = TestResultPass
				}
				return true
			},
		},

		// Does Not Self-Address An Activity
		//
		// Requires:
		// - Remote actor in the Database
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerDoesNotSelfAddress,
			Description: "Does not deliver to recipients which are the same as the actor of the Activity being notified about",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				if !hasAnyRanResult(kGETActorTestName, existing) || !hasTestPass(kGETActorTestName, existing) {
					return nil
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDoesNotSelfAddress, kServerSelfDeliverActivityKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivity(kServerSelfDeliverActivityKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send an activity from %s to both %s and %s, which attempts to send an activity to the sending actor", ctx.TestRemoteActorID, ctx.TestActor0, ctx.TestRemoteActorID),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kServerSelfDeliverActivityKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kGETActorTestName, existing) {
					return false
				} else if !hasTestPass(kGETActorTestName, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETActorTestName)
					me.State = TestResultInconclusive
					return true
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerDoesNotSelfAddress, kServerSelfDeliverActivityKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDoesNotSelfAddress) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsDirectIRI(ctx, kServerDoesNotSelfAddress)
				if err != nil {
					me.R.Add("Could not obtain activity iri for self-address test: " + err.Error())
					me.State = TestResultFail
					return true
				}
				done, at := me.helperMustGetFromDatabase(ctx, iri)
				if done {
					return true
				}
				me.R.Add("Found activity in database", at)
				activity, ok := at.(pub.Activity)
				if !ok {
					me.R.Add("Could not resolve to the pub.Activity type", iri)
					me.State = TestResultFail
					return true
				}
				found := false
				to := activity.GetActivityStreamsTo()
				if to != nil {
					for iter := to.Begin(); iter != to.End(); iter = iter.Next() {
						toIRI, err := pub.ToId(iter)
						if err != nil {
							me.R.Add("Could not convert a `to` value to an IRI")
							me.State = TestResultFail
							return true
						}
						if toIRI.String() == ctx.TestRemoteActorID.String() {
							found = true
							break
						}
					}
				}
				cc := activity.GetActivityStreamsCc()
				if cc != nil {
					for iter := cc.Begin(); iter != cc.End(); iter = iter.Next() {
						ccIRI, err := pub.ToId(iter)
						if err != nil {
							me.R.Add("Could not convert a `cc` value to an IRI")
							me.State = TestResultFail
							return true
						}
						if ccIRI.String() == ctx.TestRemoteActorID.String() {
							found = true
							break
						}
					}
				}
				if found {
					me.R.Add("The activity self addressed to the test actor", iri)
					me.State = TestResultFail
				} else {
					me.R.Add("The activity did not self address to the test actor", iri)
					me.State = TestResultPass
				}
				return true
			},
		},

		// TODO: Should: Check the "Block" action above and see if it was actually delivered to us

		// TODO: Must: Fetch remote peer's inbox and ensure every ID is unique

		// TODO: We need to note to the test end-user that shared-inbox tests are NOT supported.

		// TODO: Non-Normative: Server filters incoming content both by local untrusted users and any remote users through some sort of spam filter
		// TODO: Non-Normative: By default, implementation does not make HTTP requests to localhost when delivering Activities
		// TODO: Non-Normative: Implementation applies a whitelist of allowed URI protocols before issuing requests, e.g. for inbox delivery
		// TODO: Should: Server Filters Inbox Based On Federating Requester's Permission
		// TODO: Non-normative: Server verifies that the new content is really posted by the actor indicated in Objects received in inbox
	}
}

/* SOCIAL TESTS */

func newSocialTests() []Test {
	// TODO: Port more tests here

	// TODO: Non-Normative: Server filters incoming content both by local untrusted users and any remote users through some sort of spam filter
	// TODO: Non-Normative: By default, implementation does not make HTTP requests to localhost when delivering Activities
	// TODO: Should: Server Filters Inbox Based On Social Requester's Permission
	// TODO: Non-normative: Server verifies that the new content is really posted by the actor indicated in Objects received in outbox
	return nil
}
