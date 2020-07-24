package server

import (
	"bytes"
	"context"
	"crypto"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-fed/activity/pub"
	"github.com/go-fed/activity/streams"
	"github.com/go-fed/activity/streams/vocab"
	"github.com/go-fed/httpsig"
)

type PlainTransport struct {
	client        *http.Client
	appAgent      string
	r             *Recorder
	acceptProfile bool
}

func NewPlainTransportWithActivityJSON(r *Recorder) *PlainTransport {
	return &PlainTransport{
		client:        &http.Client{},
		appAgent:      "testserver (go-fed/testsuite)",
		r:             r,
		acceptProfile: false,
	}
}

func NewPlainTransport(r *Recorder) *PlainTransport {
	return &PlainTransport{
		client:        &http.Client{},
		appAgent:      "testserver (go-fed/testsuite)",
		r:             r,
		acceptProfile: true,
	}
}

func (p PlainTransport) DereferenceWithStatusCode(c context.Context, iri *url.URL) ([]byte, int, error) {
	req, err := http.NewRequest("GET", iri.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	req = req.WithContext(c)
	if p.acceptProfile {
		req.Header.Add("Accept", "application/ld+json; profile=\"https://www.w3.org/ns/activitystreams\"")
	} else {
		req.Header.Add("Accept", "application/activity+json")
	}
	req.Header.Add("Accept-Charset", "utf-8")
	req.Header.Add("Date", time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05")+" GMT")
	req.Header.Add("User-Agent", p.appAgent)
	p.r.Add("PlainTransport about to issue request", req)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, err := ioutil.ReadAll(resp.Body)
	return b, resp.StatusCode, err
}

func (p PlainTransport) Dereference(c context.Context, iri *url.URL) ([]byte, error) {
	req, err := http.NewRequest("GET", iri.String(), nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(c)
	if p.acceptProfile {
		req.Header.Add("Accept", "application/ld+json; profile=\"https://www.w3.org/ns/activitystreams\"")
	} else {
		req.Header.Add("Accept", "application/activity+json")
	}
	req.Header.Add("Accept-Charset", "utf-8")
	req.Header.Add("Date", time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05")+" GMT")
	req.Header.Add("User-Agent", p.appAgent)
	p.r.Add("PlainTransport about to issue request", req)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET request to %s failed (%d): %s", iri.String(), resp.StatusCode, resp.Status)
	}
	return ioutil.ReadAll(resp.Body)
}

// isSuccess returns true if the HTTP status code is either OK, Created, or
// Accepted.
func isSuccess(code int) bool {
	return code == http.StatusOK ||
		code == http.StatusCreated ||
		code == http.StatusAccepted
}

func (p PlainTransport) Deliver(c context.Context, b []byte, to *url.URL) error {
	req, err := http.NewRequest("POST", to.String(), bytes.NewReader(b))
	if err != nil {
		return err
	}
	req = req.WithContext(c)
	req.Header.Add("Content-Type", "application/ld+json; profile=\"https://www.w3.org/ns/activitystreams\"")
	req.Header.Add("Accept-Charset", "utf-8")
	req.Header.Add("Date", time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05")+" GMT")
	req.Header.Add("User-Agent", fmt.Sprintf("%s %s", p.appAgent))
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if !isSuccess(resp.StatusCode) {
		return fmt.Errorf("POST request to %s failed (%d): %s", to.String(), resp.StatusCode, resp.Status)
	}
	return nil
}

func (p PlainTransport) BatchDeliver(c context.Context, b []byte, recipients []*url.URL) error {
	var wg sync.WaitGroup
	errCh := make(chan error, len(recipients))
	for _, recipient := range recipients {
		wg.Add(1)
		go func(r *url.URL) {
			defer wg.Done()
			if err := p.Deliver(c, b, r); err != nil {
				errCh <- err
			}
		}(recipient)
	}
	wg.Wait()
	errs := make([]string, 0, len(recipients))
outer:
	for {
		select {
		case e := <-errCh:
			errs = append(errs, e.Error())
		default:
			break outer
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("batch deliver had at least one failure: %s", strings.Join(errs, "; "))
	}
	return nil
}

func HTTPSigTransport(c context.Context, a *ActorMapping) (t pub.Transport, err error) {
	prefs := []httpsig.Algorithm{httpsig.RSA_SHA256}
	digestPref := httpsig.DigestSha256
	getHeadersToSign := []string{httpsig.RequestTarget, "Date"}
	postHeadersToSign := []string{httpsig.RequestTarget, "Date", "Digest"}
	getSigner, _, err := httpsig.NewSigner(prefs, digestPref, getHeadersToSign, httpsig.Signature, 3600)
	if err != nil {
		return
	}
	postSigner, _, err := httpsig.NewSigner(prefs, digestPref, postHeadersToSign, httpsig.Signature, 3600)
	if err != nil {
		return
	}

	var pubKeyID string
	var privKey crypto.PrivateKey
	pubKeyID, _, privKey, err = a.GetKeyInfo(c)
	if err != nil {
		return
	}

	client := &http.Client{
		Timeout: time.Second * 30,
	}
	t = pub.NewHttpSigTransport(
		client,
		"go-fed/testsuite",
		&Clock{},
		getSigner,
		postSigner,
		pubKeyID,
		privKey)
	return
}

func verifyHttpSignatures(c context.Context,
	host string,
	client *http.Client,
	r *http.Request,
	a *ActorMapping) (remoteActor *url.URL, authenticated bool, err error) {
	// 1. Figure out what key we need to verify
	var v httpsig.Verifier
	v, err = httpsig.NewVerifier(r)
	if err != nil {
		return
	}
	kId := v.KeyId()
	var kIdIRI *url.URL
	kIdIRI, err = url.Parse(kId)
	if err != nil {
		return
	}
	// ASSUMPTION: Key is a fragment ID on the actor
	// No time to be robust here.
	remoteActor = &url.URL{
		Scheme: kIdIRI.Scheme,
		Host:   kIdIRI.Host,
		Path:   kIdIRI.Path,
	}
	// 2. Get our user's credentials
	var pubKeyURLString string
	var privKey crypto.PrivateKey
	_, pubKeyURLString, privKey, err = a.GetKeyInfo(c)
	if err != nil {
		return
	}
	var pubKeyURL *url.URL
	pubKeyURL, err = url.Parse(pubKeyURLString)
	if err != nil {
		return
	}
	pubKeyId := pubKeyURL.String()
	// 3. Fetch the public key of the other actor using our credentials
	prefs := []httpsig.Algorithm{httpsig.RSA_SHA256}
	digestPref := httpsig.DigestSha256
	getHeadersToSign := []string{httpsig.RequestTarget, "Date"}
	postHeadersToSign := []string{httpsig.RequestTarget, "Date", "Digest"}
	getSigner, _, err := httpsig.NewSigner(prefs, digestPref, getHeadersToSign, httpsig.Signature, 3600)
	if err != nil {
		return
	}
	postSigner, _, err := httpsig.NewSigner(prefs, digestPref, postHeadersToSign, httpsig.Signature, 3600)
	if err != nil {
		return
	}
	tp := pub.NewHttpSigTransport(
		client,
		host,
		&Clock{},
		getSigner,
		postSigner,
		pubKeyId,
		privKey)
	var b []byte
	b, err = tp.Dereference(c, kIdIRI)
	if err != nil {
		return
	}
	pKey, err := getPublicKeyFromResponse(c, b, kIdIRI)
	if err != nil {
		return
	}
	// 4. Verify the other actor's key
	algo := prefs[0]
	verErr := v.Verify(pKey, algo)
	authenticated = nil == verErr
	return
}

type publicKeyer interface {
	GetW3IDSecurityV1PublicKey() vocab.W3IDSecurityV1PublicKeyProperty
}

func getPublicKeyFromResponse(c context.Context, b []byte, keyId *url.URL) (p crypto.PublicKey, err error) {
	m := make(map[string]interface{}, 0)
	err = json.Unmarshal(b, &m)
	if err != nil {
		return
	}
	var t vocab.Type
	t, err = streams.ToType(c, m)
	if err != nil {
		return
	}
	pker, ok := t.(publicKeyer)
	if !ok {
		err = fmt.Errorf("ActivityStreams type cannot be converted to one known to have publicKey property: %T", t)
		return
	}
	pkp := pker.GetW3IDSecurityV1PublicKey()
	if pkp == nil {
		err = fmt.Errorf("publicKey property is not provided")
		return
	}
	var pkpFound vocab.W3IDSecurityV1PublicKey
	for pkpIter := pkp.Begin(); pkpIter != pkp.End(); pkpIter = pkpIter.Next() {
		if !pkpIter.IsW3IDSecurityV1PublicKey() {
			continue
		}
		pkValue := pkpIter.Get()
		var pkId *url.URL
		pkId, err = pub.GetId(pkValue)
		if err != nil {
			return
		}
		if pkId.String() != keyId.String() {
			continue
		}
		pkpFound = pkValue
		break
	}
	if pkpFound == nil {
		err = fmt.Errorf("cannot find publicKey with id: %s", keyId)
		return
	}
	pkPemProp := pkpFound.GetW3IDSecurityV1PublicKeyPem()
	if pkPemProp == nil || !pkPemProp.IsXMLSchemaString() {
		err = fmt.Errorf("publicKeyPem property is not provided or it is not embedded as a value")
		return
	}
	pubKeyPem := pkPemProp.Get()
	var block *pem.Block
	block, _ = pem.Decode([]byte(pubKeyPem))
	if block == nil || block.Type != "PUBLIC KEY" {
		err = fmt.Errorf("could not decode publicKeyPem to PUBLIC KEY pem block type")
		return
	}
	p, err = x509.ParsePKIXPublicKey(block.Bytes)
	return
}
