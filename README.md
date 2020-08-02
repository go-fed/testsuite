# testsuite

An unofficial partially-automated test suite meant to approximate the official
test suite at [test.activitypub.rocks](http://test.activitypub.rocks/).

The [official test suite](https://github.com/w3c/activitypub/issues/337)
is known to be down.

While it would be nice to get that one back up, it also only partially automated
only the C2S tests. This test suite aims to partially automate C2S, S2S, and
common tests.

Contributions needed & welcome.

See [this go-fed issue](https://github.com/go-fed/activity/issues/46)
for the old test suite's lists of tests.

## How To Use

Go to [test.activitypub.dev](https://test.activitypub.dev/) and follow the
instructions.

If you'd like to run it yourself, you will need a free domain name under which
to run this server because sending anything over the localhost interface is
explicitly a failing test case. You will also need a set of TLS keys and
certificates, I recommend [Let's Encrypt](https://letsencrypt.org/).

```
go get github.com/go-fed/testsuite
go install github.com/go-fed/testsuite
./$GOPATH/bin/testsuite \
  -cert $CERT_FULLCHAIN_FILE \
  -key $TLS_PRIVATE_KEY \
  -host $MY_DNS_HOSTNAME \
  -notify_name $MY_ALIAS \
  -notify_link $LINK_TO_MY_CONTACT_INFO
```

There are other flags but their defaults will suffice.

## Status

In "alpha" development.

Ready:

* Common Tests have been ported. Some became split into S2S/C2S test variants.
* Added option for Webfinger to be supported in a test run.
* Some S2S tests.

Left to do:

* Continue implementing S2S tests
* Implement all C2S tests
* Add option for verifying inbound HTTP Signatures
* Add option for using outbound HTTP Signatures

## Design

When a new test is started, a temporary TestRunner is set up. It is isolated
from all other TestRunners, with its own in-memory database, and is short-lived
for about fifteen minutes. It also stands up temporary fake Actors, so a test
run is itself a fully-fledged federating S2S ActivityPub application.

The tests are repeatedly iterated through to self-apply automatically, or
to await further input from the end-user to run more automated tests, or to
await triggers from the end-user's federated software.

## Future Improvements

This testsuite could also host tests for ActivityPub clients in the future, the
"C" side of C2S since go-fed supports the "S" side of both C2S and S2S. These
tests were not included in the original test suite.
