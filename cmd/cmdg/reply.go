package main

import (
	"context"
	"fmt"
	"net/textproto"
	"regexp"
	"strings"

	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"

	"github.com/ThomasHabets/cmdg/pkg/cmdg"
	"github.com/ThomasHabets/cmdg/pkg/dialog"
	"github.com/ThomasHabets/cmdg/pkg/input"
)

const (
	replyPrefix   = "Re: "
	forwardPrefix = "Fwd: "
	spaces        = " \t"
)

var (
	replyPrefixes   = regexp.MustCompile(`(?i)^(Re|Sv|Aw): `)
	forwardPrefixes = regexp.MustCompile(`(?i)^(Fwd): `)
	removeCharsRE   = regexp.MustCompile(`\r`)

	headerInReplyTo  = textproto.CanonicalMIMEHeaderKey("In-Reply-To")
	headerReferences = textproto.CanonicalMIMEHeaderKey("References")
	headerMessageID  = textproto.CanonicalMIMEHeaderKey("Message-ID")
)

// senderForReply picks the send-as address that matches one of the incoming
// message's To/CC/Delivered-To headers, so replies automatically use the
// alias the message was originally sent to.
func senderForReply(ctx context.Context, conn *cmdg.CmdG, msg *cmdg.Message) *cmdg.SendAsAddress {
	addrs, err := conn.GetSendAsAddresses(ctx)
	if err != nil || len(addrs) == 0 {
		return nil
	}
	for _, hdr := range []string{"To", "CC", "Delivered-To", "X-Original-To"} {
		v, err := msg.GetHeader(ctx, hdr)
		if err != nil || v == "" {
			continue
		}
		v = strings.ToLower(v)
		for _, a := range addrs {
			if strings.Contains(v, strings.ToLower(a.Email)) {
				return a
			}
		}
	}
	// Fall back to the default send-as address.
	for _, a := range addrs {
		if a.IsDefault {
			return a
		}
	}
	return addrs[0]
}

func replyQuoted(s string) string {
	lines := strings.Split(removeCharsRE.ReplaceAllString(s, ""), "\n")
	var ret []string
	for _, l := range lines {
		space := " "
		if strings.HasPrefix(l, ">") {
			space = ""
		}
		ret = append(ret, strings.TrimRight(">"+space+l, spaces))
	}
	return strings.Join(ret, "\n")
}

// Args:
//
//	msg: Message to reply or forward.
func replyOrForward(ctx context.Context, conn *cmdg.CmdG, keys *input.Input, to, cc, subjPrefix string, rmPrefix *regexp.Regexp, msg *cmdg.Message, attachments []*file) error {
	b, err := msg.GetUnpatchedBody(ctx)
	if err != nil {
		return err
	}
	subj, err := msg.GetSubject(ctx)
	if err != nil {
		return err
	}
	date, err := msg.GetTime(ctx)
	if err != nil {
		return err
	}
	orig, err := msg.GetHeader(ctx, "From")
	if err != nil {
		return err
	}
	threadID, err := msg.ThreadID(ctx)
	if err != nil {
		return err
	}

	headers := []string{
		fmt.Sprintf("To: %s", to),
	}
	if len(cc) != 0 {
		headers = append(headers, fmt.Sprintf("CC: %s", cc))
	}
	headers = append(headers, fmt.Sprintf("Subject: %s%s", subjPrefix, rmPrefix.ReplaceAllString(subj, "")))

	sendAsAddr := senderForReply(ctx, conn, msg)
	if addr := formatSendAsAddr(sendAsAddr); addr != "" {
		headers = append(headers, fmt.Sprintf("From: %s", addr))
	} else if d := conn.GetDefaultSender(); d != "" {
		headers = append(headers, fmt.Sprintf("From: %s", d))
	}

	refs, _ := msg.GetReferences(ctx)
	if msgID, err := msg.GetHeader(ctx, headerMessageID); err != nil {
		log.Errorf("Failed to get message ID when replying: %v", err)
		if len(refs) > 0 {
			headers = append(headers, fmt.Sprintf("References: %s", strings.Join(refs, " ")))
		}
	} else {
		headers = append(headers, fmt.Sprintf("In-Reply-To: %s", msgID))
		headers = append(headers, fmt.Sprintf("References: %s", strings.Join(append(refs, msgID), " ")))
	}

	body := []string{
		fmt.Sprintf("On %s, %s said:", date.Format("Mon, 2 Jan 2006 15:04:05 -0700"), orig),
		replyQuoted(b),
	}
	if signature != "" {
		body = append(body, "\n--\n"+signature+"\n")
	}

	prefill := strings.Join(headers, "\n") + "\n\n" + strings.Join(body, "\n")

	return compose(ctx, conn, nil, keys, threadID, prefill, attachments)
}

func reply(ctx context.Context, conn *cmdg.CmdG, keys *input.Input, msg *cmdg.Message) error {
	to, err := msg.GetReplyTo(ctx)
	if err != nil {
		return err
	}
	return replyOrForward(ctx, conn, keys, to, "", replyPrefix, replyPrefixes, msg, nil)
}

func replyAll(ctx context.Context, conn *cmdg.CmdG, keys *input.Input, msg *cmdg.Message) error {
	addrs, err := conn.GetSendAsAddresses(ctx)
	if err != nil {
		log.Errorf("Failed to fetch send-as addresses for reply-all filtering: %v", err)
	}
	var ownAddrs []string
	for _, a := range addrs {
		ownAddrs = append(ownAddrs, a.Email)
	}
	to, cc, err := msg.GetReplyToAll(ctx, ownAddrs)
	if err != nil {
		return err
	}
	return replyOrForward(ctx, conn, keys, to, cc, replyPrefix, replyPrefixes, msg, nil)
}

func forward(ctx context.Context, conn *cmdg.CmdG, keys *input.Input, msg *cmdg.Message) error {
	// Get recipient
	to, err := dialog.MultiSelection(dialog.Strings2Options(conn.Contacts()), "To> ", keys)
	if err == dialog.ErrAborted {
		return nil
	} else if err != nil {
		return err
	}
	if strings.EqualFold(to, "me") {
		p, err := conn.GetProfile(ctx)
		if err != nil {
			return errors.Wrap(err, "failed to get own email address")
		}
		to = p.EmailAddress
	}

	var atts []*file
	as, err := msg.Attachments(ctx)
	if err == nil {
		for _, a := range as {
			b, dlErr := a.Download(ctx)
			if dlErr == nil && a.Part != nil {
				atts = append(atts, &file{name: a.Part.Filename, content: b})
			} else if dlErr != nil {
				log.Errorf("Failed to download attachment %q: %v", a.Part.Filename, dlErr)
			}
		}
	} else {
		log.Errorf("Failed to get attachments: %v", err)
	}

	return replyOrForward(ctx, conn, keys, to, "", forwardPrefix, forwardPrefixes, msg, atts)
}
