package rustfs

import "testing"

func TestValidateBucketName(t *testing.T) {
	ok := []string{"abc", "my-bucket", "a.b.c", "data01"}
	for _, n := range ok {
		if err := ValidateBucketName(n); err != nil {
			t.Fatalf("%q: %v", n, err)
		}
	}
	bad := []string{"", "AB", "a", "MyBucket", "-bad", "bad-", "xn--foo", "a..b"}
	for _, n := range bad {
		if err := ValidateBucketName(n); err == nil {
			t.Fatalf("%q: expected error", n)
		}
	}
}

func TestNormalizePrefix(t *testing.T) {
	if got := normalizePrefix(""); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := normalizePrefix("a/b"); got != "a/b/" {
		t.Fatalf("got %q", got)
	}
	if got := normalizePrefix("/a/b/"); got != "a/b/" {
		t.Fatalf("got %q", got)
	}
}

func TestParseConfig(t *testing.T) {
	c := ParseConfig(`{"port":"9002","access_key":"k","secret_key":"s"}`)
	if c.Port != "9002" || c.AccessKey != "k" || c.SecretKey != "s" {
		t.Fatalf("%+v", c)
	}
	c = ParseConfig("")
	if c.AccessKey != "rustfsadmin" || c.Port != "9000" {
		t.Fatalf("%+v", c)
	}
}
