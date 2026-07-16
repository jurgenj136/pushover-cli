package resolver

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseCredentialsJSONStrictFraming(t *testing.T) {
	token, user := strings.Repeat("a", 30), strings.Repeat("b", 30)
	valid := `{"token":"` + token + `","user_key":"` + user + `"}`
	if got, err := parseCredentialsJSON(strings.NewReader(valid)); err != nil || got.Credentials.Token != token || got.Alias != "stdin" {
		t.Fatalf("valid credentials = %#v, %v", got, err)
	}
	for _, input := range []string{
		`{"token":"","token":"` + token + `","user_key":"` + user + `"}`,
		`{"token":"` + token + `","token":"` + token + `","user_key":"` + user + `"}`,
		`{"token":"` + token + `","user_key":"` + user + `","extra":"x"}`,
		`{"token":null,"user_key":"` + user + `"}`,
		`{"token":"` + token + `","user_key":2}`,
		valid + ` {}`,
		`[]`,
	} {
		if _, err := parseCredentialsJSON(strings.NewReader(input)); err == nil {
			t.Fatalf("accepted %q", input)
		}
	}
	padded := valid + strings.Repeat(" ", maxCredentialsBytes-len(valid))
	if _, err := parseCredentialsJSON(strings.NewReader(padded)); err != nil {
		t.Fatalf("rejected exact 4096-byte credentials: %v", err)
	}
	if _, err := parseCredentialsJSON(bytes.NewReader(append([]byte(padded), ' '))); err == nil {
		t.Fatal("accepted oversized credentials")
	}
	if _, err := parseCredentialsJSON(bytes.NewReader([]byte{0xff})); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
}
