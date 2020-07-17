package server

import (
	"fmt"
	"net/url"
)

type link struct {
	Rel      string `json:"rel,omitempty"`
	Type     string `json:"type,omitempty"`
	Href     string `json:"href,omitempty"`
	Template string `json:"template,omitempty"`
}

type webfinger struct {
	Subject string   `json:"subject,omitempty"`
	Aliases []string `json:"aliases,omitempty"`
	Links   []link   `json:"links,omitempty"`
}

func toWebfinger(host, username string, apIRI *url.URL) webfinger {
	return webfinger{
		Subject: fmt.Sprintf("acct:%s@%s", username, host),
		Aliases: []string{
			apIRI.String(),
		},
		Links: []link{
			{
				Rel:  "self",
				Type: "application/activity+json",
				Href: apIRI.String(),
			},
		},
	}
}

// RFC 6415
func hostMeta(host string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<XRD xmlns="http://docs.oasis-open.org/ns/xri/xrd-1.0">
  <Link rel="lrdd" type="application/xrd+xml" template="https://%s/.well-known/webfinger?resource={uri}"/>
</XRD>`, host)
}
