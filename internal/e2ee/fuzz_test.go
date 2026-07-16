package e2ee

import "testing"

func FuzzEnvelope(f *testing.F) {
	f.Add("")
	f.Add("%%%")
	f.Add("AAECAwQFBgcICQoLDA0ODxp8+AImNhKqB8XJHQ7KgmHtOLS5OO41YvUpTp74x8dAL0ISu8/oTuygFVzOYBHaVyClwr7F+tGKc4hcclvFsS7nkcQNiftMdIdmE8hmk/N/")
	key, err := ParseKey(testKey)
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, encoded string) {
		if len(encoded) > 8*1024 {
			t.Skip()
		}
		_, _ = decodeEnvelope(key, encoded)
	})
}
