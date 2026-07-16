package domain

import "testing"

func FuzzValidateMessage(f *testing.F) {
	f.Add("body", "title", "https://example.com", "open", 0)
	f.Fuzz(func(t *testing.T, body, title, rawURL, urlTitle string, priority int) {
		if len(body)+len(title)+len(rawURL)+len(urlTitle) > 16*1024 {
			t.Skip()
		}
		_ = ValidateMessage(Message{Body: body, Title: title, URL: rawURL, URLTitle: urlTitle, Priority: priority})
	})
}
