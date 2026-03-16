package slackbot

import "github.com/ca-srg/ragent/internal/pkg/slacksearch"

type RateLimiter = slacksearch.RateLimiter

var NewRateLimiter = slacksearch.NewRateLimiter
