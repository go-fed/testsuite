package server

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

const (
	kPathPrefixTests         = "/tests/evaluate/"
	kPathHome                = "/"
	kPathTestNew             = "/tests/new"
	kPathTestState           = "/tests/status/"
	kPathInstructionResponse = "/tests/instructions/"
	kPathHostMeta            = "/.well-known/host-meta"
	kPathWebfinger           = "/.well-known/webfinger"
)

const (
	TmplHome       = "home.tmpl"
	TmplNewTest    = "new_test.tmpl"
	TmplTestStatus = "test_status.tmpl"
)

type WebServer struct {
	hostname string
	tmpl     *template.Template
	s        *http.Server
	ts       *TestServer
}

func NewWebServer(tmpl *template.Template,
	s *http.Server,
	hostname string,
	testTimeout time.Duration,
	maxTests int) *WebServer {
	ws := &WebServer{
		hostname: hostname,
		tmpl:     tmpl,
		s:        s,
		ts:       NewTestServer(hostname, kPathPrefixTests, testTimeout, maxTests),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(kPathHome, ws.homepageHandler)
	mux.HandleFunc(kPathPrefixTests, ws.testRequestHandler)
	mux.HandleFunc(kPathTestState, ws.testStatusHandler)
	mux.HandleFunc(kPathTestNew, ws.startTestHandler)
	mux.HandleFunc(kPathInstructionResponse, ws.instructionResponseHandler)
	mux.HandleFunc(kPathHostMeta, ws.hostMetaHandler)
	mux.HandleFunc(kPathWebfinger, ws.webfingerHandler)
	s.Handler = mux
	s.RegisterOnShutdown(ws.shutdown)
	return ws
}

func (ws *WebServer) shutdown() {
	ws.ts.shutdown()
}

func (ws *WebServer) homepageHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		ws.tmpl.ExecuteTemplate(w, TmplHome, nil)
	} else {
		http.NotFound(w, r)
	}
}

func (ws *WebServer) startTestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		ws.tmpl.ExecuteTemplate(w, TmplNewTest, nil)
	} else if r.Method == http.MethodPost {
		remoteActorIRI := r.PostFormValue("remote_actor_iri")
		testRemoteActorID, err := url.Parse(remoteActorIRI)
		if err != nil {
			http.Error(w, "Error parsing remote actor IRI: "+err.Error(), http.StatusBadRequest)
			return
		}
		c2sStr := r.PostFormValue("enable_social")
		s2sStr := r.PostFormValue("enable_federating")
		enableWebfingerStr := r.PostFormValue("enable_webfinger")
		c2s := c2sStr == "true"
		s2s := s2sStr == "true"
		enableWebfinger := enableWebfingerStr == "true"
		testNumber := rand.Int()
		pathPrefix := path.Join(kPathPrefixTests, fmt.Sprintf("%d", testNumber))
		err = ws.ts.StartTest(r.Context(),
			pathPrefix,
			c2s,
			s2s,
			enableWebfinger,
			testRemoteActorID)
		if err != nil {
			http.Error(w, "Error preparing test: "+err.Error(), http.StatusInternalServerError)
			return
		}
		redir := &url.URL{
			Path: path.Join(kPathTestState, fmt.Sprintf("%d", testNumber)),
		}
		http.Redirect(w, r, redir.String(), http.StatusFound)
	} else {
		http.NotFound(w, r)
	}
}

func (ws *WebServer) testRequestHandler(w http.ResponseWriter, r *http.Request) {
	prefix, ok := PathToTestPathPrefix(r.URL)
	if !ok {
		http.NotFound(w, r)
		return
	}
	c := context.WithValue(r.Context(), kContextKeyTestPrefix, prefix)
	ws.ts.HandleWeb(c, w, r)
}

func (ws *WebServer) testStatusHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		pathPrefix, ok := StatePathToTestPathPrefix(r.URL)
		if !ok {
			http.NotFound(w, r)
			return
		}
		state, ok := ws.ts.TestState(pathPrefix)
		if !ok {
			http.NotFound(w, r)
			return
		}
		ws.tmpl.ExecuteTemplate(w, TmplTestStatus, state)
	} else {
		http.NotFound(w, r)
	}
}

func (ws *WebServer) instructionResponseHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		pathPrefix, ok := InstructionResponsePathToTestPathPrefix(r.URL)
		if !ok {
			http.NotFound(w, r)
			return
		}
		statePath, ok := InstructionResponsePathToTestState(r.URL)
		if !ok {
			http.NotFound(w, r)
			return
		}
		err := r.ParseForm()
		if err != nil {
			http.Error(w, "Error parsing form: "+err.Error(), http.StatusBadRequest)
		}
		ws.ts.HandleInstructionResponse(pathPrefix, r.Form)
		redir := &url.URL{
			Path: statePath,
		}
		http.Redirect(w, r, redir.String(), http.StatusFound)
	} else {
		http.NotFound(w, r)
	}
}

func (ws *WebServer) hostMetaHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/xrd+xml")
	hm := hostMeta(ws.hostname)
	io.WriteString(w, hm)
}

const (
	// This is an unreserved character of RFC 3986 Section 2.3
	kWebfingerTestDelim = "."
)

func (ws *WebServer) webfingerHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	userHost := strings.Split(
		strings.TrimPrefix(q.Get("resource"), "acct:"),
		"@")
	if len(userHost) != 2 {
		http.Error(w, "Error parsing query: "+q.Get("resource"), http.StatusBadRequest)
		return
	}
	userTest := strings.Split(userHost[0], kWebfingerTestDelim)
	if len(userTest) != 2 {
		http.Error(w, "Error parsing test and user: "+userHost[0], http.StatusBadRequest)
		return
	}
	user := userTest[0]
	pathPrefix := testPathPrefixFromId(userTest[1])
	username, apIRI, err := ws.ts.HandleWebfinger(pathPrefix, user)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	wf := toWebfinger(ws.hostname, username, apIRI)
	b, err := json.Marshal(wf)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/jrd+json")
	w.Write(b)
}
