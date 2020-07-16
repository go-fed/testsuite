package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

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
	textBoxInstructionResponse  instructionResponseType = "text_box"
	checkBoxInstructionResponse instructionResponseType = "checkbox"
)

const (
	kSkipKeyIdSuffix      = "_skip"
	kDereferenceIRIKeyId  = "instruction_key_dereference_iri"
	kTombstoneIRIKeyId    = "instruction_key_tombstone_iri"
	kUsingTombstonesKeyId = "instruction_key_using_tombstones"
	kNeverExistedIRIKeyId = "instruction_key_never_existed_iri"
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

type TestRunnerContext struct {
	// Set by the server
	TestRemoteActorID *url.URL
	Actor             pub.FederatingActor
	DB                *Database
	AM                *ActorMapping
	TestActor0        *url.URL
	TestActor1        *url.URL
	TestActor2        *url.URL
	TestActor3        *url.URL
	TestActor4        *url.URL
	// Set by the TestRunner
	C context.Context
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

func (b *baseTest) helperToOrderedCollectionOrPage(ctx *TestRunnerContext, t vocab.Type) (done bool, oc vocab.Type) {
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

/* TEST HELPERS & CONSTANTS */

const (
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
kServerObjectNeverExisted = "Server Handles When Object Never Existed"
kServer404NotFoundNeverExisted = "Server Responds With 404 Not Found When Object Never Existed"
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
				/*ctx.AM.AddContextInfoForTest(ctx)
				ptp, err := HTTPSigTransport(ctx.C, ctx.AM)
				if err != nil {
					me.R.Add("Could not create http signature transport: ", err)
					me.State = TestResultFail
					return true
				}*/
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

		// Server Handles When Object Never Existed"
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

		// Server Responds 404 Gone For Objects That Never Existed 
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

		// TODO: Should: Instructions to get a private IRI & whether 403 or 404
		// TODO: Should: Run Test for 404 status
		// TODO: Should: Run Test for 403 status

		/* BREAK THESE DOWN MORE*/

		// TODO: Non-Normative: By default, implementation does not make HTTP requests to localhost when delivering Activities
		// TODO: Non-Normative: Implementation applies a whitelist of allowed URI protocols before issuing requests, e.g. for inbox delivery
		// TODO: Non-Normative: Server filters incoming content both by local untrusted users and any remote users through some sort of spam filter
	}
}

/* FEDERATING TESTS */

func newFederatingTests() []Test {
	// TODO: Port more tests here

	// TODO: Should: Server Filters Inbox Based On Federating Requester's Permission
	// TODO: Non-normative: Server verifies that the new content is really posted by the actor indicated in Objects received in inbox
	return nil
}

/* SOCIAL TESTS */

func newSocialTests() []Test {
	// TODO: Port more tests here

	// TODO: Should: Server Filters Inbox Based On Social Requester's Permission
	// TODO: Non-normative: Server verifies that the new content is really posted by the actor indicated in Objects received in outbox
	return nil
}
