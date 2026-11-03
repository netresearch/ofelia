// Copyright (c) 2025-2026 Netresearch DTT GmbH
// SPDX-License-Identifier: MIT

package middlewares

import (
	"encoding/base64"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"testing"
	"time"

	"github.com/armon/circbuf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/netresearch/ofelia/core"
)

// sentMail is a delivered message reduced to what the tests assert on.
type sentMail struct {
	body        string   // decoded text/html body part
	attachments []string // attachment file names
}

// sendWithOutput runs the Mail middleware against the test SMTP server with
// the given mail-output-in-body value and job output, and returns the
// message the server received.
func sendWithOutput(t *testing.T, outputInBody *int, stdout, stderr string) sentMail {
	t.Helper()
	f := setupMailTest(t)

	f.ctx.Start()
	_, _ = f.ctx.Execution.OutputStream.Write([]byte(stdout))
	_, _ = f.ctx.Execution.ErrorStream.Write([]byte(stderr))
	f.ctx.Stop(nil)

	m := NewMail(&MailConfig{
		SMTPHost:         f.smtpdHost,
		SMTPPort:         f.smtpdPort,
		EmailTo:          "foo@foo.com",
		EmailFrom:        "qux@qux.com",
		SMTPTLSPolicy:    SMTPTLSPolicyNone, // test fixture: plaintext SMTP, see #653
		MailOutputInBody: outputInBody,
	})

	done := make(chan error, 1)
	go func() { done <- m.Run(f.ctx) }()

	select {
	case <-f.fromCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for SMTP server to receive MAIL FROM")
	}

	var raw string
	select {
	case raw = <-f.dataCh:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for email data")
	}
	require.NoError(t, <-done)

	return parseSentMail(t, raw)
}

// parseSentMail decodes the multipart message built by go-mail.
func parseSentMail(t *testing.T, raw string) sentMail {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(raw))
	require.NoError(t, err)

	mediaType, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(mediaType, "multipart/"), "got %s", mediaType)

	var out sentMail
	mr := multipart.NewReader(msg.Body, params["boundary"])
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)

		if name := part.FileName(); name != "" {
			out.attachments = append(out.attachments, name)
			continue
		}
		var r io.Reader = part
		switch part.Header.Get("Content-Transfer-Encoding") {
		case "quoted-printable":
			r = quotedprintable.NewReader(part)
		case "base64":
			r = base64.NewDecoder(base64.StdEncoding, part)
		}
		b, err := io.ReadAll(r)
		require.NoError(t, err)
		// Quoted-printable carries line breaks as CRLF on the wire.
		out.body = strings.ReplaceAll(string(b), "\r\n", "\n")
	}
	return out
}

func hasAttachmentSuffix(attachments []string, suffix string) bool {
	for _, a := range attachments {
		if strings.HasSuffix(a, suffix) {
			return true
		}
	}
	return false
}

func TestMailOutputInBody_UnsetOrZeroKeepsAttachments(t *testing.T) {
	t.Parallel()

	for name, value := range map[string]*int{"unset": nil, "zero": new(0)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := sendWithOutput(t, value, "out line\n", "err line\n")

			assert.True(t, hasAttachmentSuffix(got.attachments, ".stdout.log"), got.attachments)
			assert.True(t, hasAttachmentSuffix(got.attachments, ".stderr.log"), got.attachments)
			assert.True(t, hasAttachmentSuffix(got.attachments, ".json"), got.attachments)
			assert.NotContains(t, got.body, "out line")
			assert.NotContains(t, got.body, "err line")
		})
	}
}

func TestMailOutputInBody_LastNLines(t *testing.T) {
	t.Parallel()
	got := sendWithOutput(t, new(2), "line1\nline2\nline3\n", "err1\n")

	assert.False(t, hasAttachmentSuffix(got.attachments, ".stdout.log"), got.attachments)
	assert.False(t, hasAttachmentSuffix(got.attachments, ".stderr.log"), got.attachments)
	assert.True(t, hasAttachmentSuffix(got.attachments, ".json"),
		"the JSON attachment with job metadata stays")

	assert.NotContains(t, got.body, "line1")
	assert.Contains(t, got.body, "<b>stdout</b> (truncated):")
	assert.Contains(t, got.body, "<pre>line2\nline3</pre>")
	assert.Contains(t, got.body, "<b>stderr</b>:")
	assert.Contains(t, got.body, "<pre>err1\n</pre>")
	// The job summary stays in the body.
	assert.Contains(t, got.body, "Execution <b>successful</b>")
}

func TestMailOutputInBody_AllLines(t *testing.T) {
	t.Parallel()
	got := sendWithOutput(t, new(-1), "line1\nline2\nline3\n", "")

	assert.False(t, hasAttachmentSuffix(got.attachments, ".stdout.log"), got.attachments)
	assert.Contains(t, got.body, "<pre>line1\nline2\nline3\n</pre>")
	assert.NotContains(t, got.body, "(truncated)")
	assert.NotContains(t, got.body, "<b>stderr</b>", "an empty stream gets no section")
}

func TestMailOutputInBody_StderrOnly(t *testing.T) {
	t.Parallel()
	got := sendWithOutput(t, new(5), "", "boom\n")

	assert.NotContains(t, got.body, "<b>stdout</b>")
	assert.Contains(t, got.body, "<b>stderr</b>:")
	assert.Contains(t, got.body, "<pre>boom\n</pre>")
}

func TestMailOutputInBody_EscapesHTML(t *testing.T) {
	t.Parallel()
	got := sendWithOutput(t, new(-1), "<script>alert(1)</script>\n", "")

	assert.NotContains(t, got.body, "<script>")
	assert.Contains(t, got.body, "&lt;script&gt;alert(1)&lt;/script&gt;")
}

func TestTailLines(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		in      string
		n       int
		want    string
		wantCut bool
	}{
		{"fewer lines than n", "a\nb\n", 5, "a\nb\n", false},
		{"exactly n lines", "a\nb\nc\n", 3, "a\nb\nc\n", false},
		{"exactly n lines without trailing newline", "a\nb\nc", 3, "a\nb\nc", false},
		{"more lines than n", "a\nb\nc\n", 2, "b\nc", true},
		{"more lines than n without trailing newline", "a\nb\nc", 2, "b\nc", true},
		{"one line", "a\nb\nc\n", 1, "c", true},
		{"negative means all", "a\nb\nc\n", -1, "a\nb\nc\n", false},
		{"zero returns input", "a\nb\nc\n", 0, "a\nb\nc\n", false},
		{"empty lines count", "a\n\n\nb\n", 2, "\nb", true},
		{"leading newline", "\nb", 1, "b", true},
		{"empty", "", 3, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, cut := tailLines(tc.in, tc.n)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.wantCut, cut)
		})
	}
}

// TestOutputSections_BufferOverflowMarksTruncated covers the bounded output
// buffer: when the job wrote more than the buffer holds, the section is
// marked truncated even though no line was dropped by the line limit.
func TestOutputSections_BufferOverflowMarksTruncated(t *testing.T) {
	t.Parallel()

	small, err := circbuf.NewBuffer(8)
	require.NoError(t, err)
	_, _ = small.Write([]byte("0123456789\nabc\n"))

	exact, err := circbuf.NewBuffer(8)
	require.NoError(t, err)
	_, _ = exact.Write([]byte("1234567\n"))

	e := &core.Execution{OutputStream: small, ErrorStream: exact}
	sections := outputSections(e, -1)

	require.Len(t, sections, 2)
	assert.Equal(t, "stdout", sections[0].Name)
	assert.True(t, sections[0].Truncated, "buffer overflow must be marked")
	assert.Equal(t, "stderr", sections[1].Name)
	assert.False(t, sections[1].Truncated, "a buffer filled exactly is complete")
}
