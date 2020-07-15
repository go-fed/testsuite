package server

import (
	"context"
	"fmt"
	"html/template"
	"math/rand"
	"net/http"
	"net/url"
	"path"
	"time"
)

const (
	kPathPrefixTests         = "/tests/evaluate/"
	kPathHome                = "/"
	kPathTestNew             = "/tests/new"
	kPathTestState           = "/tests/status/"
	kPathInstructionResponse = "/tests/instructions/"
)

const (
	TmplHome       = "home.tmpl"
	TmplNewTest    = "new_test.tmpl"
	TmplTestStatus = "test_status.tmpl"
)

type WebServer struct {
	tmpl *template.Template
	s    *http.Server
	ts   *TestServer
}

func NewWebServer(tmpl *template.Template,
	s *http.Server,
	hostname string,
	testTimeout time.Duration,
	maxTests int) *WebServer {
	ws := &WebServer{
		tmpl: tmpl,
		s:    s,
		ts:   NewTestServer(hostname, kPathPrefixTests, testTimeout, maxTests),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(kPathHome, ws.homepageHandler)
	mux.HandleFunc(kPathPrefixTests, ws.testRequestHandler)
	mux.HandleFunc(kPathTestState, ws.testStatusHandler)
	mux.HandleFunc(kPathTestNew, ws.startTestHandler)
	mux.HandleFunc(kPathInstructionResponse, ws.instructionResponseHandler)
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
		c2s := c2sStr == "true"
		s2s := s2sStr == "true"
		testNumber := rand.Int()
		pathPrefix := path.Join(kPathPrefixTests, fmt.Sprintf("%d", testNumber))
		err = ws.ts.StartTest(r.Context(), pathPrefix, c2s, s2s, testRemoteActorID)
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
