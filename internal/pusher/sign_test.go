package pusher

import (
	"net/url"
	"testing"
	"time"
)

// Values from the Pusher HTTP API and channel authorization reference.
const (
	docKey    = "278d425bdf160c739803"
	docSecret = "7ad3773142a6692b25b8"
)

func TestSignedQueryMatchesReference(t *testing.T) {
	body := []byte(`{"name":"foo","channels":["project-3"],"data":"{\"some\":\"data\"}"}`)
	q := signedQuery(docKey, docSecret, "POST", "/apps/3/events", url.Values{}, body, time.Unix(1353088179, 0))
	if got, want := q.Get("body_md5"), "ec365a775a4cd0599faeb73354201b6f"; got != want {
		t.Fatalf("body_md5 = %s, want %s", got, want)
	}
	if got, want := q.Get("auth_signature"), "da454824c97ba181a32ccc17a72625ba02771f50b50e1e7430e47a1f3f457e6c"; got != want {
		t.Fatalf("auth_signature = %s, want %s", got, want)
	}
}

func TestChannelAuthMatchesReference(t *testing.T) {
	got := ChannelAuth(docKey, docSecret, "1234.1234", "private-foobar", "")
	want := docKey + ":58df8b0c36d6982b82c3ecf6b4662e34fe8c25bba48f5369f135bf843651c3a4"
	if got != want {
		t.Fatalf("auth = %s, want %s", got, want)
	}
}

func TestSocketURL(t *testing.T) {
	got := SocketURL(Credentials{Key: "k", Host: "localhost:8080", Scheme: "http"}, "hibira-cli", "1.0.0")
	want := "ws://localhost:8080/app/k?client=hibira-cli&flash=false&protocol=7&version=1.0.0"
	if got != want {
		t.Fatalf("url = %s, want %s", got, want)
	}
}
