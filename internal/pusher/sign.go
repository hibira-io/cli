// Package pusher holds the small part of the Pusher Channels protocol the
// CLI needs: signed HTTP API requests and a WebSocket subscriber.
package pusher

import (
	"crypto/hmac"
	"crypto/md5" //nolint:gosec // body_md5 is required by the Pusher HTTP API
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// sign returns hex(HMAC-SHA256(secret, message)).
func sign(secret, message string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(message))
	return hex.EncodeToString(m.Sum(nil))
}

// canonicalQuery sorts the parameters by key and joins them without
// URL-escaping, which is what the Pusher signature is computed over.
func canonicalQuery(q url.Values) string {
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+q.Get(k))
	}
	return strings.Join(parts, "&")
}

// signedQuery adds the auth_* parameters (and body_md5 when there is a
// body) to q for a request to path.
func signedQuery(key, secret, method, path string, q url.Values, body []byte, now time.Time) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[strings.ToLower(k)] = v
	}
	out.Set("auth_key", key)
	out.Set("auth_timestamp", strconv.FormatInt(now.Unix(), 10))
	out.Set("auth_version", "1.0")
	if len(body) > 0 {
		sum := md5.Sum(body) //nolint:gosec // see import comment
		out.Set("body_md5", hex.EncodeToString(sum[:]))
	}
	toSign := strings.ToUpper(method) + "\n" + path + "\n" + canonicalQuery(out)
	out.Set("auth_signature", sign(secret, toSign))
	return out
}

// ChannelAuth returns the auth value for subscribing socketID to a private
// or presence channel. channelData is only used for presence channels.
func ChannelAuth(key, secret, socketID, channel, channelData string) string {
	s := socketID + ":" + channel
	if channelData != "" {
		s += ":" + channelData
	}
	return key + ":" + sign(secret, s)
}
