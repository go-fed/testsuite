package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
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
	numberInstructionResponse                             = "number"
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
	kNumDuplicateActivitiesKeyId        = "instruction_key_federated_duplicate_activity_num"
	kSentAcceptFollowKeyId              = "test_key_federated_sent_follow_for_accept"
	kAcceptFollowKeyId                  = "instruction_key_federated_accept_follow"
	kSentRejectFollowKeyId              = "test_key_federated_sent_follow_for_reject"
	kRejectFollowKeyId                  = "instruction_key_federated_reject_follow"
	kFollowToAcceptKeyId                = "instruction_key_federated_follow_to_accept"
	kFollowToRejectKeyId                = "instruction_key_federated_follow_to_reject"
	kSentReplyAcceptToTheirFollowKeyId  = "test_key_federated_accept_follow_sent"
	kSentReplyRejectToTheirFollowKeyId  = "test_key_federated_reject_follow_sent"
	kFollowingCollectionKeyId           = "test_key_following_collection_iri_list"
	kActivityWithFollowersKeyId         = "instruction_key_federated_activity_with_followers"
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

type Transporter interface {
	NewTransport(context.Context, *url.URL, string) (pub.Transport, error)
}

type APHooks interface {
	ExpectFederatedCoreActivity(keyID string)
	ExpectFederatedCoreActivityHTTPSigsMustMatchTestRemoteActor(keyID string)
	ExpectFederatedCoreActivityCreate(keyID string)
	ExpectFederatedCoreActivityUpdate(keyID string)
	ExpectFederatedCoreActivityDelete(keyID string)
	ExpectFederatedCoreActivityFollow(keyID string)
	ExpectFederatedCoreActivityAccept(keyID string)
	ExpectFederatedCoreActivityReject(keyID string)
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
	Transporter           Transporter
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

func (b *baseTest) helperActivityAddressedTo(ctx *TestRunnerContext, a pub.Activity, toFind *url.URL) (done, found bool) {
	to := a.GetActivityStreamsTo()
	if to != nil {
		for iter := to.Begin(); iter != to.End(); iter = iter.Next() {
			toIRI, err := pub.ToId(iter)
			if err != nil {
				b.R.Add("Could not convert a `to` value to an IRI")
				b.State = TestResultFail
				done = true
				return
			}
			if toIRI.String() == toFind.String() {
				found = true
				return
			}
		}
	}
	cc := a.GetActivityStreamsCc()
	if cc != nil {
		for iter := cc.Begin(); iter != cc.End(); iter = iter.Next() {
			ccIRI, err := pub.ToId(iter)
			if err != nil {
				b.R.Add("Could not convert a `cc` value to an IRI")
				b.State = TestResultFail
				done = true
				return
			}
			if ccIRI.String() == toFind.String() {
				found = true
				return
			}
		}
	}
	return
}

func (b *baseTest) helperCollectionToIRIs(ctx *TestRunnerContext, t vocab.Type) (done bool, iris []*url.URL) {
	// TODO: Handle pagination
	done = false
	var c vocab.ActivityStreamsCollection
	var oc vocab.ActivityStreamsOrderedCollection
	c, _ = t.(vocab.ActivityStreamsCollection)
	oc, _ = t.(vocab.ActivityStreamsOrderedCollection)
	if c != nil {
		it := c.GetActivityStreamsItems()
		if it == nil {
			b.R.Add("OrderedCollection has no 'items' property")
			b.State = TestResultFail
			done = true
			return
		}
		iter := it.Begin()
		done, iris = b.helperIdPropertyToIRIs(ctx, func() pub.IdProperty {
			ret := iter
			if ret == it.End() {
				ret = nil
			} else {
				iter = iter.Next()
			}
			return ret
		})
	} else if oc != nil {
		oit := oc.GetActivityStreamsOrderedItems()
		if oit == nil {
			b.R.Add("OrderedCollection has no 'orderedItems' property")
			b.State = TestResultFail
			done = true
			return
		}
		iter := oit.Begin()
		done, iris = b.helperIdPropertyToIRIs(ctx, func() pub.IdProperty {
			ret := iter
			if ret == oit.End() {
				ret = nil
			} else {
				iter = iter.Next()
			}
			return ret
		})
	} else {
		b.R.Add("ActivityStreams Type is neither a Collection nor OrderedCollection", t)
		b.State = TestResultFail
		done = true
	}
	return
}

func (b *baseTest) helperIdPropertyToIRIs(ctx *TestRunnerContext, f func() pub.IdProperty) (done bool, iris []*url.URL) {
	done = false
	i := f()
	for i != nil {
		iri, err := pub.ToId(i)
		if err != nil {
			b.R.Add("Cannot find the id of an element")
			b.State = TestResultFail
			done = true
		}
		iris = append(iris, iri)
		i = f()
	}
	return
}

func (b *baseTest) helperGetActivityFromInstructionKey(ctx *TestRunnerContext, keyId string) (done bool, activity pub.Activity) {
	iri, err := getInstructionResponseAsDirectIRI(ctx, keyId)
	if err != nil {
		b.R.Add("Could not resolve the ID of the activity: " + err.Error())
		b.State = TestResultFail
		done = true
		return
	}
	var t vocab.Type
	done, t = b.helperMustGetFromDatabase(ctx, iri)
	if done {
		return
	}
	ok := false
	activity, ok = t.(pub.Activity)
	if !ok {
		b.R.Add("Could not resolve to the pub.Activity type", iri)
		b.State = TestResultFail
		done = true
		return
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
	kServerOutboxDeliveredActivities                = "Outbox Contains Delivered Activities"
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
	kServerShouldNotDeliverBlocks                   = "Should Not Deliver Blocks"
	kDeliverCreateArticlesToTestPeer                = "Receives Create Activity For Article From Federated Peer"
	kDedupedActorInboxTestName                      = "Dedupes Actor Inbox"
	kSendFollowRequestForAcceptance                 = "Send A Follow Request For Acceptance"
	kServerAcceptsFollowRequest                     = "Accept A Follow Request"
	kSendFollowRequestForRejecting                  = "Send A Follow Request For Rejecting"
	kServerRejectsFollowRequest                     = "Reject A Follow Request"
	kGETFollowersCollection                         = "GET Followers Collection"
	kServerHandlesReceivingAcceptFollow             = "Receives Accept-Follow Activity From Federated Peer"
	kServerHandlesReceivingRejectFollow             = "Receives Reject-Follow Activity From Federated Peer"
	kGETFollowingCollection                         = "GET Following Collection"
	kAddedActorToFollowingCollection                = "Following Collection Has Accepted-Follow Actor"
	kDidNotAddActorToFollowingCollection            = "Following Collection Does Not Have Rejected-Follow Actor"
	kServerSendsActivityWithFollowersAddressed      = "Sends Activity With Followers Addressed"
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

func getInstructionResponseAsNonNegativeInt(ctx *TestRunnerContext, keyID string) (n int, err error) {
	var s string
	s, err = getInstructionResponseAsOnlyString(ctx, keyID)
	if err != nil {
		return
	}
	n, err = strconv.Atoi(s)
	if err != nil {
		return
	}
	if n < 0 {
		err = fmt.Errorf("%s is negative: %d", keyID, n)
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

		// Outbox Contains Delivered Activities
		//
		// Requires:
		// - Peer actor sent activity to us
		// - Outbox in the Database
		// - Outbox ID in the context
		// Side Effects:
		// - N/a
		&baseTest{
			TestName:    kServerOutboxDeliveredActivities,
			Description: "The delivered activity is present in the actor's outbox",
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
				done, activity := me.helperGetActivityFromInstructionKey(ctx, kDeliveredFederatedActivityToKeyId)
				if done {
					return true
				}
				to := activity.GetActivityStreamsTo()
				if to == nil {
					me.R.Add("Activity has no `to` value", activity)
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
					me.R.Add("Could not find the actor in the `to` property of the activity", ctx.TestActor0, activity)
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
				done, activity := me.helperGetActivityFromInstructionKey(ctx, kDeliveredFederatedActivityCcKeyId)
				if done {
					return true
				}
				cc := activity.GetActivityStreamsCc()
				if cc == nil {
					me.R.Add("Activity has no `cc` value", activity)
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
					me.R.Add("Could not find the actor in the `cc` property of the activity", ctx.TestActor0, activity)
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
				done, activity := me.helperGetActivityFromInstructionKey(ctx, kDeliveredFederatedActivityBtoKeyId)
				if done {
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
				done, activity := me.helperGetActivityFromInstructionKey(ctx, kDeliveredFederatedActivityBccKeyId)
				if done {
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
				if !hasAnyRanResult(kServerOutboxDeliveredActivities, existing) ||
					!hasAnyRanResult(kServerDeliversActivityTo, existing) ||
					!hasAnyRanResult(kServerDeliversActivityCc, existing) ||
					!hasAnyRanResult(kServerDeliversActivityBto, existing) ||
					!hasAnyRanResult(kServerDeliversActivityBcc, existing) {
					return false
				}
				var keysToExamine []string
				if hasTestPass(kServerOutboxDeliveredActivities, existing) {
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
						kServerOutboxDeliveredActivities,
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
				if !hasAnyInstructionKeys(ctx, kServerDeliversBlockWithObject, []string{kServerBlockActivityKeyId, kServerBlockActivityDoneKeyId}, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerDeliversBlockWithObject) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				if hasAnyInstructionKey(ctx, kServerDeliversBlockWithObject, kServerBlockActivityDoneKeyId, skippable) {
					me.R.Add("Done button pressed.")
					me.State = TestResultPass
				} else {
					me.R.Add("Callback was successfully called after examining the activity.")
					me.State = TestResultPass
				}
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
				done, activity := me.helperGetActivityFromInstructionKey(ctx, kServerDoesNotSelfAddress)
				if done {
					return true
				}
				done, found := me.helperActivityAddressedTo(ctx, activity, ctx.TestRemoteActorID)
				if done {
					return true
				}
				if found {
					me.R.Add("The activity self addressed to the test actor", activity)
					me.State = TestResultFail
				} else {
					me.R.Add("The activity did not self address to the test actor", activity)
					me.State = TestResultPass
				}
				return true
			},
		},

		// Should Not Deliver Blocks
		//
		// Requires:
		// - Delivers Block With Object
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerShouldNotDeliverBlocks,
			Description: "Delivers activity with 'object' property if the Activity type is a Block",
			SpecKind:    TestSpecKindShould,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kServerDeliversBlockWithObject, existing) {
					return false
				} else if !hasTestPass(kServerDeliversBlockWithObject, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kServerDeliversBlockWithObject)
					me.State = TestResultInconclusive
					return true
				}
				iri, err := getInstructionResponseAsDirectIRI(ctx, kServerBlockActivityKeyId)
				if err == nil {
					me.R.Add("Received a federated Block activity", iri)
					me.State = TestResultFail
				} else {
					me.R.Add("Did not receive a federated Block activity")
					me.State = TestResultPass
				}
				return true
			},
		},

		// Delivers Create Activity For Article To Federated Peer
		//
		// Requires:
		// - Remote actor in the Database
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kDeliverCreateArticlesToTestPeer,
			Description: "Deliver Create-Articles To Federated Peer",
			SpecKind:    TestSpecKindMust,
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
				peerInboxIRI, err := pub.ToId(inbox)
				if err != nil {
					me.R.Add("Could not determine the ID of the actor's inbox: ", err)
					me.State = TestResultFail
					return true
				}
				outboxIRI := ActorIRIToOutboxIRI(ctx.TestActor1.ActivityPubIRI)
				// Construct an article to send.
				article := streams.NewActivityStreamsArticle()
				cp := streams.NewActivityStreamsContentProperty()
				cp.AppendXMLSchemaString("<html><body>Hello, world!</body></html>")
				article.SetActivityStreamsContent(cp)
				mediaType := streams.NewActivityStreamsMediaTypeProperty()
				mediaType.Set("text/html; charset=utf-8")
				article.SetActivityStreamsMediaType(mediaType)
				attrTo := streams.NewActivityStreamsAttributedToProperty()
				attrTo.AppendIRI(ctx.TestActor1.ActivityPubIRI)
				article.SetActivityStreamsAttributedTo(attrTo)
				toProp := streams.NewActivityStreamsToProperty()
				publicIRI, err := url.Parse(pub.PublicActivityPubIRI)
				if err != nil {
					me.R.Add("Could not parse public activity IRI", err)
					me.State = TestResultFail
					return true
				}
				toProp.AppendIRI(publicIRI)
				toProp.AppendIRI(ctx.TestRemoteActorID)
				article.SetActivityStreamsTo(toProp)
				const n = 3
				var sa pub.Activity
				var b []byte
				tp, err := ctx.Transporter.NewTransport(ctx.C, outboxIRI, "go-fed")
				if err != nil {
					me.R.Add("Could not build a new transport", err)
					me.State = TestResultFail
					return true
				}
				for i := 0; i < n; i++ {
					if i == 0 {
						sa, err = ctx.Actor.Send(ctx.C, outboxIRI, article)
					} else {
						// Resend same activity, to ensure it is
						// deduplicated by peer
						err = tp.Deliver(ctx.C, b, peerInboxIRI)
					}
					if err != nil {
						me.R.Add("Could not deliver activity to the test actor", err)
						me.State = TestResultFail
						return true
					} else if i == 0 {
						m, err := streams.Serialize(sa)
						if err != nil {
							me.R.Add("Could not serialize activity", err)
							me.State = TestResultFail
							return true
						}
						b, err = json.Marshal(m)
						if err != nil {
							me.R.Add("Could not JSON Marshal activity", err)
							me.State = TestResultFail
							return true
						}
					}
					me.R.Add("Successfully sent the (re)sent the same activity", i, sa)
				}
				me.R.Add(fmt.Sprintf("Successfully sent the same activity %d times", n))
				me.State = TestResultPass
				return true
			},
		},

		// Dedupes Actor Inbox
		//
		// Requires:
		// - Actor in the Database
		// - We sent activities to peer
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kDedupedActorInboxTestName,
			Description: "Deduplicates activities returned by the inbox by comparing activity ids",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				if !hasAnyRanResult(kGETActorTestName, existing) || !hasTestPass(kGETActorTestName, existing) ||
					!hasAnyRanResult(kDeliverCreateArticlesToTestPeer, existing) || !hasTestPass(kDeliverCreateArticlesToTestPeer, existing) {
					return nil
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kDedupedActorInboxTestName, kNumDuplicateActivitiesKeyId, skippable) {
					return &Instruction{
						Instructions: fmt.Sprintf("How many activities are in the inbox for %s from %s?", ctx.TestRemoteActorID, ctx.TestActor1),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kNumDuplicateActivitiesKeyId,
							Type: numberInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kGETActorTestName, existing) {
					return false
				} else if !hasAnyRanResult(kDeliverCreateArticlesToTestPeer, existing) {
					return false
				} else if !hasTestPass(kGETActorTestName, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETActorTestName)
					me.State = TestResultInconclusive
					return true
				} else if !hasTestPass(kDeliverCreateArticlesToTestPeer, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kDeliverCreateArticlesToTestPeer)
					me.State = TestResultInconclusive
					return true
				}
				n, err := getInstructionResponseAsNonNegativeInt(ctx, kNumDuplicateActivitiesKeyId)
				if err == nil {
					me.R.Add("Error processing instructions", err)
					me.State = TestResultFail
					return true
				} else if n != 1 {
					me.R.Add("Did not deduplicate the same activity in the inbox", n)
					me.State = TestResultFail
					return true
				}
				me.R.Add("Peer deduplicated the delivery to just one activity")
				me.State = TestResultPass
				return true
			},
		},

		// Send A Follow Request For Acceptance
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kSendFollowRequestForAcceptance,
			Description: "Can deliver a follow request to the federated peer, for acceptance",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				// Get our information
				outboxIRI := ActorIRIToOutboxIRI(ctx.TestActor1.ActivityPubIRI)
				// Construct a Follow to send
				follow := streams.NewActivityStreamsFollow()
				attrTo := streams.NewActivityStreamsAttributedToProperty()
				attrTo.AppendIRI(ctx.TestActor1.ActivityPubIRI)
				follow.SetActivityStreamsAttributedTo(attrTo)
				actorp := streams.NewActivityStreamsActorProperty()
				actorp.AppendIRI(ctx.TestActor1.ActivityPubIRI)
				follow.SetActivityStreamsActor(actorp)
				objp := streams.NewActivityStreamsObjectProperty()
				objp.AppendIRI(ctx.TestRemoteActorID)
				follow.SetActivityStreamsObject(objp)
				toProp := streams.NewActivityStreamsToProperty()
				toProp.AppendIRI(ctx.TestRemoteActorID)
				follow.SetActivityStreamsTo(toProp)
				// Send the Follow to the peer
				sa, err := ctx.Actor.Send(ctx.C, outboxIRI, follow)
				if err != nil {
					me.R.Add("Could not deliver activity to the test actor", err)
					me.State = TestResultFail
					return true
				}
				newIRI, err := pub.GetId(sa)
				if err != nil {
					me.R.Add("Could not obtain sent activity iri: " + err.Error())
					me.State = TestResultFail
					return true
				}
				ctx.C = context.WithValue(ctx.C, kSentAcceptFollowKeyId, newIRI)
				me.R.Add(fmt.Sprintf("Successfully sent the follow request", sa))
				me.State = TestResultPass
				return true
			},
		},

		// Accept A Follow Request
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerAcceptsFollowRequest,
			Description: "Accept A Follow Request",
			SpecKind:    TestSpecKindShould,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				if !hasAnyRanResult(kSendFollowRequestForAcceptance, existing) || !hasTestPass(kSendFollowRequestForAcceptance, existing) {
					return nil
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerAcceptsFollowRequest, kAcceptFollowKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivityAccept(kAcceptFollowKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please accept the follow request from %s to %s?", ctx.TestActor1, ctx.TestRemoteActorID),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kAcceptFollowKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				// Get the accept we received
				iri, err := getInstructionResponseAsDirectIRI(ctx, kAcceptFollowKeyId)
				if err != nil {
					me.R.Add("Could not obtain an activity iri: " + err.Error())
					me.State = TestResultFail
					return true
				}
				done, t := me.helperMustGetFromDatabase(ctx, iri)
				if done {
					return true
				}
				accept, ok := t.(vocab.ActivityStreamsAccept)
				if !ok {
					me.R.Add("Could not resolve Activity to an Accept", iri)
					me.State = TestResultFail
					return true
				}
				// Get the id of the activity we sent
				firi, err := getInstructionResponseAsDirectIRI(ctx, kSentAcceptFollowKeyId)
				if err != nil {
					me.R.Add("Could not obtain our activity iri: " + err.Error())
					me.State = TestResultFail
					return true
				}
				// Ensure our follow is the object of the accept
				obj := accept.GetActivityStreamsObject()
				if obj == nil {
					me.R.Add("Accept has no 'object' property")
					me.State = TestResultFail
					return true
				}
				found := false
				for iter := obj.Begin(); iter != obj.End(); iter = iter.Next() {
					iteriri, err := pub.ToId(iter)
					if err != nil {
						me.R.Add("Cannot get id of an element of the 'object' property")
						me.State = TestResultFail
						return true
					}
					if iteriri.String() == firi.String() {
						found = true
					} else {
						me.R.Add("Warning: found an unexpected object", iteriri)
					}
				}
				if found {
					me.R.Add("Successfully received an Accept with the object as our Follow")
					me.State = TestResultPass
				} else {
					me.R.Add("Did not find our Follow as an object of the Accept")
					me.State = TestResultFail
				}
				return true
			},
		},

		// Send A Follow Request For Rejection
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kSendFollowRequestForRejecting,
			Description: "Can deliver a follow request to the federated peer, for rejection",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				// Get our information
				outboxIRI := ActorIRIToOutboxIRI(ctx.TestActor2.ActivityPubIRI)
				// Construct a Follow to send
				follow := streams.NewActivityStreamsFollow()
				attrTo := streams.NewActivityStreamsAttributedToProperty()
				attrTo.AppendIRI(ctx.TestActor2.ActivityPubIRI)
				follow.SetActivityStreamsAttributedTo(attrTo)
				actorp := streams.NewActivityStreamsActorProperty()
				actorp.AppendIRI(ctx.TestActor2.ActivityPubIRI)
				follow.SetActivityStreamsActor(actorp)
				objp := streams.NewActivityStreamsObjectProperty()
				objp.AppendIRI(ctx.TestRemoteActorID)
				follow.SetActivityStreamsObject(objp)
				toProp := streams.NewActivityStreamsToProperty()
				toProp.AppendIRI(ctx.TestRemoteActorID)
				follow.SetActivityStreamsTo(toProp)
				// Send the Follow to the peer
				sa, err := ctx.Actor.Send(ctx.C, outboxIRI, follow)
				if err != nil {
					me.R.Add("Could not deliver activity to the test actor", err)
					me.State = TestResultFail
					return true
				}
				newIRI, err := pub.GetId(sa)
				if err != nil {
					me.R.Add("Could not obtain sent activity iri: " + err.Error())
					me.State = TestResultFail
					return true
				}
				ctx.C = context.WithValue(ctx.C, kSentRejectFollowKeyId, newIRI)
				me.R.Add(fmt.Sprintf("Successfully sent the follow request", sa))
				me.State = TestResultPass
				return true
			},
		},

		// Reject A Follow Request
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerRejectsFollowRequest,
			Description: "Reject A Follow Request",
			SpecKind:    TestSpecKindShould,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				if !hasAnyRanResult(kSendFollowRequestForRejecting, existing) || !hasTestPass(kSendFollowRequestForRejecting, existing) {
					return nil
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerRejectsFollowRequest, kRejectFollowKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivityReject(kRejectFollowKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please reject the follow request from %s to %s?", ctx.TestActor2, ctx.TestRemoteActorID),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kRejectFollowKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				// Get the reject we received
				iri, err := getInstructionResponseAsDirectIRI(ctx, kRejectFollowKeyId)
				if err != nil {
					me.R.Add("Could not obtain an activity iri: " + err.Error())
					me.State = TestResultFail
					return true
				}
				done, t := me.helperMustGetFromDatabase(ctx, iri)
				if done {
					return true
				}
				reject, ok := t.(vocab.ActivityStreamsReject)
				if !ok {
					me.R.Add("Could not resolve Activity to an Reject", iri)
					me.State = TestResultFail
					return true
				}
				// Get the id of the activity we sent
				firi, err := getInstructionResponseAsDirectIRI(ctx, kSentRejectFollowKeyId)
				if err != nil {
					me.R.Add("Could not obtain our activity iri: " + err.Error())
					me.State = TestResultFail
					return true
				}
				// Ensure our follow is the object of the reject
				obj := reject.GetActivityStreamsObject()
				if obj == nil {
					me.R.Add("Reject has no 'object' property")
					me.State = TestResultFail
					return true
				}
				found := false
				for iter := obj.Begin(); iter != obj.End(); iter = iter.Next() {
					iteriri, err := pub.ToId(iter)
					if err != nil {
						me.R.Add("Cannot get id of an element of the 'object' property")
						me.State = TestResultFail
						return true
					}
					if iteriri.String() == firi.String() {
						found = true
					} else {
						me.R.Add("Warning: found an unexpected object", iteriri)
					}
				}
				if found {
					me.R.Add("Successfully received an Reject with the object as our Follow")
					me.State = TestResultPass
				} else {
					me.R.Add("Did not find our Follow as an object of the Reject")
					me.State = TestResultFail
				}
				return true
			},
		},

		// GET Followers Collection
		//
		// Requires:
		// - Actor in the Database
		// - Acceptance and/or Rejecting of Follow(s)
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kGETFollowersCollection,
			Description: "Server responds to GET request at followers URL & has the appropriate test actors",
			SpecKind:    TestSpecKindShould,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kGETActorTestName, existing) || !hasAnyRanResult(kServerAcceptsFollowRequest, existing) || !hasAnyRanResult(kServerRejectsFollowRequest, existing) {
					return false
				} else if !hasTestPass(kGETActorTestName, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETActorTestName)
					me.State = TestResultInconclusive
					return true
				}
				checkAcceptActor1 := hasTestPass(kServerAcceptsFollowRequest, existing)
				checkRejectActor2 := hasTestPass(kServerRejectsFollowRequest, existing)
				if checkAcceptActor1 {
					me.R.Add(fmt.Sprintf("Will check for accepted actor because previous test passed: %s", kServerAcceptsFollowRequest))
				} else {
					me.R.Add(fmt.Sprintf("Will not check for accepted actor because previous test did not pass: %s", kServerAcceptsFollowRequest))
				}
				if checkRejectActor2 {
					me.R.Add(fmt.Sprintf("Will check for rejected actor because previous test passed: %s", kServerRejectsFollowRequest))
				} else {
					me.R.Add(fmt.Sprintf("Will not check for rejected actor because previous test did not pass: %s", kServerRejectsFollowRequest))
				}
				if !checkAcceptActor1 && !checkRejectActor2 {
					me.R.Add("Skipping: dependency tests did not pass")
					me.State = TestResultInconclusive
					return true
				}
				// Get the actor followers IRI
				done, at := me.helperMustGetFromDatabase(ctx, ctx.TestRemoteActorID)
				if done {
					return true
				}
				done, actor := me.helperToActor(ctx, at)
				if done {
					return true
				}
				f := actor.GetActivityStreamsFollowers()
				if f == nil {
					me.R.Add("Actor at IRI does not have a followers collection: ", ctx.TestRemoteActorID)
					me.State = TestResultFail
					return true
				}
				fid, err := pub.ToId(f)
				if err != nil {
					me.R.Add("Could not determine the ID of the actor's followers: ", err)
					me.State = TestResultFail
					return true
				}
				// Dereference the collection.
				ptp := NewPlainTransport(me.R)
				me.R.Add(fmt.Sprintf("About to dereference followers at %s", fid))
				done, t := me.helperDereference(ctx, fid, ptp)
				if done {
					return true
				}
				done, iris := me.helperCollectionToIRIs(ctx, t)
				if done {
					return true
				}
				actor1Found := false
				actor2Found := false
				for _, iri := range iris {
					if iri.String() == ctx.TestActor1.ActivityPubIRI.String() {
						actor1Found = true
					}
					if iri.String() == ctx.TestActor2.ActivityPubIRI.String() {
						actor2Found = true
					}
				}
				a1pass := false
				a2pass := false
				if checkAcceptActor1 && actor1Found {
					me.R.Add("Successfully found accepted-follow actor in the followers collection", ctx.TestActor1.ActivityPubIRI)
					a1pass = true
				} else if checkAcceptActor1 && !actor1Found {
					me.R.Add("Failed by not finding the accepted-follow actor in the followers collection", ctx.TestActor1.ActivityPubIRI)
				} else {
					a1pass = true
				}
				if checkRejectActor2 && actor2Found {
					me.R.Add("Failed by finding the rejected-follow actor in the followers collection", ctx.TestActor2.ActivityPubIRI)
				} else if checkRejectActor2 && !actor2Found {
					me.R.Add("Successfully did not find rejected-follow actor in the followers collection", ctx.TestActor2.ActivityPubIRI)
					a2pass = true
				} else {
					a2pass = true
				}
				if a1pass && a2pass {
					me.State = TestResultPass
				} else {
					me.State = TestResultFail
				}
				return true
			},
		},

		// Receives Accept-Follow Activity From Federated Peer
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerHandlesReceivingAcceptFollow,
			Description: "Server can issue a Follow request and handle receiving an Accept for the Follow",
			SpecKind:    TestSpecKindShould,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerHandlesReceivingAcceptFollow, kFollowToAcceptKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivityFollow(kFollowToAcceptKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send a follow request to %s from %s", ctx.TestActor3, ctx.TestRemoteActorID),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kFollowToAcceptKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				// Get the IRI of the follow we received, make sure it is a Follow
				iri, err := getInstructionResponseAsDirectIRI(ctx, kFollowToAcceptKeyId)
				if err != nil {
					me.R.Add("Could not obtain an activity iri: " + err.Error())
					me.State = TestResultFail
					return true
				}
				done, t := me.helperMustGetFromDatabase(ctx, iri)
				if done {
					return true
				}
				follow, ok := t.(vocab.ActivityStreamsFollow)
				if !ok {
					me.R.Add("Could not resolve Activity to a Follow", iri)
					me.State = TestResultFail
					return true
				}
				me.R.Add("Received a Follow", follow)
				// Get our information
				outboxIRI := ActorIRIToOutboxIRI(ctx.TestActor3.ActivityPubIRI)
				// Construct the Accept to send.
				accept := streams.NewActivityStreamsAccept()
				attrTo := streams.NewActivityStreamsAttributedToProperty()
				attrTo.AppendIRI(ctx.TestActor3.ActivityPubIRI)
				accept.SetActivityStreamsAttributedTo(attrTo)
				actorp := streams.NewActivityStreamsActorProperty()
				actorp.AppendIRI(ctx.TestActor3.ActivityPubIRI)
				accept.SetActivityStreamsActor(actorp)
				objp := streams.NewActivityStreamsObjectProperty()
				objp.AppendActivityStreamsFollow(follow)
				accept.SetActivityStreamsObject(objp)
				toProp := streams.NewActivityStreamsToProperty()
				toProp.AppendIRI(ctx.TestRemoteActorID)
				accept.SetActivityStreamsTo(toProp)
				// Send the Accept to the peer
				sa, err := ctx.Actor.Send(ctx.C, outboxIRI, accept)
				if err != nil {
					me.R.Add("Could not deliver activity to the test actor", err)
					me.State = TestResultFail
					return true
				}
				newIRI, err := pub.GetId(sa)
				if err != nil {
					me.R.Add("Could not obtain sent activity iri: " + err.Error())
					me.State = TestResultFail
					return true
				}
				ctx.C = context.WithValue(ctx.C, kSentReplyAcceptToTheirFollowKeyId, newIRI)
				me.R.Add(fmt.Sprintf("Successfully sent the Accept for the Follow request", sa))
				me.State = TestResultPass
				return true
			},
		},

		// Receives Reject-Follow Activity From Federated Peer
		//
		// Requires:
		// - N/A
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kServerHandlesReceivingRejectFollow,
			Description: "Server can issue a Follow request and handle receiving a Reject for the Follow",
			SpecKind:    TestSpecKindShould,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerHandlesReceivingRejectFollow, kFollowToRejectKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivityFollow(kFollowToRejectKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send a follow request to %s from %s", ctx.TestActor0, ctx.TestRemoteActorID),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kFollowToRejectKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				// Get the IRI of the follow we received, make sure it is a Follow
				iri, err := getInstructionResponseAsDirectIRI(ctx, kFollowToRejectKeyId)
				if err != nil {
					me.R.Add("Could not obtain an activity iri: " + err.Error())
					me.State = TestResultFail
					return true
				}
				done, t := me.helperMustGetFromDatabase(ctx, iri)
				if done {
					return true
				}
				follow, ok := t.(vocab.ActivityStreamsFollow)
				if !ok {
					me.R.Add("Could not resolve Activity to a Follow", iri)
					me.State = TestResultFail
					return true
				}
				me.R.Add("Received a Follow", follow)
				// Get our information
				outboxIRI := ActorIRIToOutboxIRI(ctx.TestActor0.ActivityPubIRI)
				// Construct the Reject to send.
				reject := streams.NewActivityStreamsReject()
				attrTo := streams.NewActivityStreamsAttributedToProperty()
				attrTo.AppendIRI(ctx.TestActor0.ActivityPubIRI)
				reject.SetActivityStreamsAttributedTo(attrTo)
				actorp := streams.NewActivityStreamsActorProperty()
				actorp.AppendIRI(ctx.TestActor0.ActivityPubIRI)
				reject.SetActivityStreamsActor(actorp)
				objp := streams.NewActivityStreamsObjectProperty()
				objp.AppendActivityStreamsFollow(follow)
				reject.SetActivityStreamsObject(objp)
				toProp := streams.NewActivityStreamsToProperty()
				toProp.AppendIRI(ctx.TestRemoteActorID)
				reject.SetActivityStreamsTo(toProp)
				// Send the Reject to the peer
				sa, err := ctx.Actor.Send(ctx.C, outboxIRI, reject)
				if err != nil {
					me.R.Add("Could not deliver activity to the test actor", err)
					me.State = TestResultFail
					return true
				}
				newIRI, err := pub.GetId(sa)
				if err != nil {
					me.R.Add("Could not obtain sent activity iri: " + err.Error())
					me.State = TestResultFail
					return true
				}
				ctx.C = context.WithValue(ctx.C, kSentReplyRejectToTheirFollowKeyId, newIRI)
				me.R.Add(fmt.Sprintf("Successfully sent the Reject for the Follow request", sa))
				me.State = TestResultPass
				return true
			},
		},

		// GET Following Collection
		//
		// Requires:
		// - Actor in the Database
		// - Sent the Acceptance and/or Rejections of Follow(s)
		// Side Effects:
		// - Put Following IRIS into context
		&baseTest{
			TestName:    kGETFollowingCollection,
			Description: "Server responds to GET request at followers URL",
			SpecKind:    TestSpecKindShould,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kGETActorTestName, existing) || !hasAnyRanResult(kServerHandlesReceivingAcceptFollow, existing) || !hasAnyRanResult(kServerHandlesReceivingRejectFollow, existing) {
					return false
				} else if !hasTestPass(kGETActorTestName, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETActorTestName)
					me.State = TestResultInconclusive
					return true
				}
				// Get the actor followers IRI
				done, at := me.helperMustGetFromDatabase(ctx, ctx.TestRemoteActorID)
				if done {
					return true
				}
				done, actor := me.helperToActor(ctx, at)
				if done {
					return true
				}
				f := actor.GetActivityStreamsFollowing()
				if f == nil {
					me.R.Add("Actor at IRI does not have a following collection: ", ctx.TestRemoteActorID)
					me.State = TestResultFail
					return true
				}
				fid, err := pub.ToId(f)
				if err != nil {
					me.R.Add("Could not determine the ID of the actor's following collection: ", err)
					me.State = TestResultFail
					return true
				}
				// Dereference the collection.
				ptp := NewPlainTransport(me.R)
				me.R.Add(fmt.Sprintf("About to dereference following at %s", fid))
				done, t := me.helperDereference(ctx, fid, ptp)
				if done {
					return true
				}
				done, iris := me.helperCollectionToIRIs(ctx, t)
				if done {
					return true
				}
				ctx.C = context.WithValue(ctx.C, kFollowingCollectionKeyId, iris)
				me.R.Add("Successfully fetched following collection and obtained IRI list")
				me.State = TestResultPass
				return true
			},
		},

		// Following Collection Has Accepted-Follow Actor
		//
		// Requires:
		// - Following IRIS in the context
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kAddedActorToFollowingCollection,
			Description: "If an Accept is in reply to a Follow activity, adds actor to receiver's Following Collection",
			SpecKind:    TestSpecKindShould,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kGETFollowingCollection, existing) {
					return false
				} else if !hasTestPass(kGETFollowingCollection, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETFollowingCollection)
					me.State = TestResultInconclusive
					return true
				}
				// Get the following collection IRIs
				iris, err := getInstructionResponseAsSliceOfIRIs(ctx, kFollowingCollectionKeyId)
				if err != nil {
					me.R.Add("Could not obtain iri(s) for following test: " + err.Error())
					me.State = TestResultFail
					return true
				}
				// Find our actor
				found := false
				for _, iri := range iris {
					if iri.String() == ctx.TestActor3.ActivityPubIRI.String() {
						found = true
						break
					}
				}
				if found {
					me.R.Add("Successfully found our actor in the following collection", ctx.TestActor3.ActivityPubIRI)
					me.State = TestResultPass
				} else {
					me.R.Add("Failed to find our actor in the following collection", ctx.TestActor3.ActivityPubIRI)
					me.State = TestResultFail
				}
				return true
			},
		},

		// Following Collection Does Not Have Rejected-Follow Actor
		//
		// Requires:
		// - Following IRIS in the context
		// Side Effects:
		// - N/A
		&baseTest{
			TestName:    kDidNotAddActorToFollowingCollection,
			Description: "If a Reject is in reply to a Follow activity, MUST NOT add actor to receiver's Following Collection",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kGETFollowingCollection, existing) {
					return false
				} else if !hasTestPass(kGETFollowingCollection, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETFollowingCollection)
					me.State = TestResultInconclusive
					return true
				}
				// Get the following collection IRIs
				iris, err := getInstructionResponseAsSliceOfIRIs(ctx, kFollowingCollectionKeyId)
				if err != nil {
					me.R.Add("Could not obtain iri(s) for following test: " + err.Error())
					me.State = TestResultFail
					return true
				}
				// Ensure we can't find our actor
				found := false
				for _, iri := range iris {
					if iri.String() == ctx.TestActor0.ActivityPubIRI.String() {
						found = true
						break
					}
				}
				if found {
					me.R.Add("Failure because we found our actor in the following collection", ctx.TestActor0.ActivityPubIRI)
					me.State = TestResultFail
				} else {
					me.R.Add("Successful because we did not find our actor in the following collection", ctx.TestActor0.ActivityPubIRI)
					me.State = TestResultPass
				}
				return true
			},
		},

		// Send Activity With Followers Also Addressed
		//
		// Requires:
		// - GET Followers succeeded
		// Side Effects:
		// - Stores activity addressed to followers
		&baseTest{
			TestName:    kServerSendsActivityWithFollowersAddressed,
			Description: "Server sends activity with followers in `cc` or `to` on Activity",
			SpecKind:    TestSpecKindMust,
			R:           NewRecorder(),
			ShouldSendInstructions: func(me *baseTest, ctx *TestRunnerContext, existing []Result) *Instruction {
				if !hasAnyRanResult(kGETActorTestName, existing) || !hasTestPass(kGETActorTestName, existing) ||
					!hasAnyRanResult(kGETFollowersCollection, existing) || !hasTestPass(kGETFollowersCollection, existing) {
					return nil
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerSendsActivityWithFollowersAddressed, kActivityWithFollowersKeyId, skippable) {
					ctx.APH.ExpectFederatedCoreActivity(kActivityWithFollowersKeyId)
					return &Instruction{
						Instructions: fmt.Sprintf("Please send an activity from %s to %s with their followers in the `to` or `cc`", ctx.TestRemoteActorID, ctx.TestActor0),
						Skippable:    skippable,
						Resp: []instructionResponse{{
							Key:  kActivityWithFollowersKeyId,
							Type: labelOnlyInstructionResponse,
						}},
					}
				}
				return nil
			},
			Run: func(me *baseTest, ctx *TestRunnerContext, existing []Result) (returnResult bool) {
				if !hasAnyRanResult(kGETActorTestName, existing) || !hasAnyRanResult(kGETFollowersCollection, existing) {
					return false
				} else if !hasTestPass(kGETActorTestName, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETActorTestName)
					me.State = TestResultInconclusive
					return true
				} else if !hasTestPass(kGETFollowersCollection, existing) {
					me.R.Add("Skipping: dependency test did not pass: " + kGETFollowersCollection)
					me.State = TestResultInconclusive
					return true
				}
				const skippable = true
				if !hasAnyInstructionKey(ctx, kServerSendsActivityWithFollowersAddressed, kActivityWithFollowersKeyId, skippable) {
					return false
				} else if hasSkippedTestName(ctx, kServerSendsActivityWithFollowersAddressed) {
					ctx.APH.ClearExpectations()
					me.R.Add("Skipping: Instructions were skipped")
					me.State = TestResultInconclusive
					return true
				}
				// Get the peer actor's followers
				done, at := me.helperMustGetFromDatabase(ctx, ctx.TestRemoteActorID)
				if done {
					return true
				}
				done, actor := me.helperToActor(ctx, at)
				if done {
					return true
				}
				f := actor.GetActivityStreamsFollowers()
				if f == nil {
					me.R.Add("Actor at IRI does not have a followers collection", ctx.TestRemoteActorID)
					me.State = TestResultFail
					return true
				}
				fid, err := pub.ToId(f)
				if err != nil {
					me.R.Add("Could not determine the ID of the actor's followers", err)
					me.State = TestResultFail
					return true
				}
				// Get the activity sent.
				done, activity := me.helperGetActivityFromInstructionKey(ctx, kActivityWithFollowersKeyId)
				if done {
					return true
				}
				iri, err := pub.GetId(activity)
				if err != nil {
					me.R.Add("Could not determine the ID of the activity", err)
					me.State = TestResultFail
					return true
				}
				// Check that addressing has kActor0, not kActor1, not kActor2, and federated peer's followers.
				done, foundActor0 := me.helperActivityAddressedTo(ctx, activity, ctx.TestActor0.ActivityPubIRI)
				if done {
					return true
				}
				done, foundActor1 := me.helperActivityAddressedTo(ctx, activity, ctx.TestActor1.ActivityPubIRI)
				if done {
					return true
				}
				done, foundActor2 := me.helperActivityAddressedTo(ctx, activity, ctx.TestActor2.ActivityPubIRI)
				if done {
					return true
				}
				done, foundFollowers := me.helperActivityAddressedTo(ctx, activity, fid)
				if done {
					return true
				}
				if !foundActor0 {
					me.R.Add("The activity did not directly address an actor", ctx.TestActor0.ActivityPubIRI)
					me.State = TestResultFail
					return true
				}
				if foundActor1 {
					me.R.Add("The activity directly addressed an actor that would be covered by the followers collection", ctx.TestActor1.ActivityPubIRI)
					me.State = TestResultFail
					return true
				}
				if foundActor2 {
					me.R.Add("The activity directly addressed an undesired actor", ctx.TestActor2.ActivityPubIRI)
					me.State = TestResultFail
					return true
				}
				if !foundFollowers {
					me.R.Add("The activity did not directly address the required followers collection", fid)
					me.State = TestResultFail
					return true
				}
				me.R.Add(fmt.Sprintf("Obtained the federated activity", activity))
				// Ensure kActor0 (directly addressed) and kActor1 (follower) both have the activity
				actor0InboxIRI := ActorIRIToInboxIRI(ctx.TestActor0.ActivityPubIRI)
				done, at = me.helperMustGetFromDatabase(ctx, actor0InboxIRI)
				if done {
					return true
				}
				done, actor0oc := me.helperToOrderedCollectionOrPage(ctx, at)
				if done {
					return true
				}
				actor0oip := actor0oc.GetActivityStreamsOrderedItems()
				done, found := me.helperOrderedItemsHasIRI(ctx, iri, actor0InboxIRI, actor0oip)
				if done {
					return true
				} else if !found {
					me.R.Add("Actor0, directly addressed, was not delivered the activity")
					me.State = TestResultFail
					return true
				}
				me.R.Add("Actor0 has the activity, as they are directly addressed")
				actor1InboxIRI := ActorIRIToInboxIRI(ctx.TestActor1.ActivityPubIRI)
				done, at = me.helperMustGetFromDatabase(ctx, actor1InboxIRI)
				if done {
					return true
				}
				done, actor1oc := me.helperToOrderedCollectionOrPage(ctx, at)
				if done {
					return true
				}
				actor1oip := actor1oc.GetActivityStreamsOrderedItems()
				done, found = me.helperOrderedItemsHasIRI(ctx, iri, actor1InboxIRI, actor1oip)
				if done {
					return true
				} else if !found {
					me.R.Add("Actor1, a follower, was not delivered the activity")
					me.State = TestResultFail
					return true
				}
				me.R.Add("Actor1 has the activity, as they are marked as followers")
				me.State = TestResultPass
				return true
			},
		},

		// TODO: Must: Forwards incoming activities to the values of to, bto, cc, bcc, audience if and only if criteria in 7.1.2 are met.
		// By sending an activity to a peer that CC's followers (kActor1), and a non-following actor (kActor 2)

		// TODO: Must: Take care to be sure that the Update is authorized to modify its object
		// By re-sending an updated Article created in kDeliverCreateArticlesToTestPeer but from kActor0 instead of kActor1,
		// prompt the peer software to say whether the update was successful or not.
		// TODO: Should: Completely replace its copy of the activity with the newly received value
		// By re-sending an updated Article created in kDeliverCreateArticlesToTestPeer from kActor1,
		// prompt the peer software to say whether the update was successful or not.

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
