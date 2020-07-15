package server

import (
	"time"

	"github.com/go-fed/activity/pub"
)

var _ pub.Clock = &Clock{}

type Clock struct{}

func (c *Clock) Now() time.Time {
	return time.Now()
}
