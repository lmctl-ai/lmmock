package main

// Protocol mock registrations: one blank import per internal/proto package.
// Merge rule for this file: on rebase conflicts keep ALL import lines.
//
// Wave-7 protocol packages register themselves here as they land, e.g.:
//
//	_ "github.com/lmctl-ai/lmmock/internal/proto/redis"
import (
	_ "github.com/lmctl-ai/lmmock/internal/proto/imap"
	_ "github.com/lmctl-ai/lmmock/internal/proto/kafka"
	_ "github.com/lmctl-ai/lmmock/internal/proto/mongodb"
	_ "github.com/lmctl-ai/lmmock/internal/proto/mqtt"
	_ "github.com/lmctl-ai/lmmock/internal/proto/postgres"
	_ "github.com/lmctl-ai/lmmock/internal/proto/redis"
	_ "github.com/lmctl-ai/lmmock/internal/proto/smtp"
)
