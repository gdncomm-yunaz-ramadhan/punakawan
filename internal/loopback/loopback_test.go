package loopback

import "testing"

func TestValidateHost(t *testing.T) {
	cases := []struct {
		host string
		ok   bool
	}{
		{"127.0.0.1:7330", true},
		{"localhost:7330", true},
		{"[::1]:7330", true},
		{"evil.example.com", false},
		{"127.0.0.1.evil.example.com", false},
	}
	for _, c := range cases {
		err := ValidateHost(c.host)
		if (err == nil) != c.ok {
			t.Errorf("ValidateHost(%q): err=%v, want ok=%v", c.host, err, c.ok)
		}
	}
}

func TestValidateOrigin(t *testing.T) {
	if err := ValidateOrigin("", "127.0.0.1:7330"); err != nil {
		t.Errorf("empty Origin should be allowed, got %v", err)
	}
	if err := ValidateOrigin("http://127.0.0.1:7330", "127.0.0.1:7330"); err != nil {
		t.Errorf("matching loopback Origin should be allowed, got %v", err)
	}
	if err := ValidateOrigin("https://evil.example.com", "127.0.0.1:7330"); err == nil {
		t.Error("cross-origin request should be rejected")
	}
}

func TestListenerRejectsNonLoopback(t *testing.T) {
	if _, err := Listener("0.0.0.0", "0"); err == nil {
		t.Error("expected non-loopback bind address to be rejected")
	}
	l, err := Listener("127.0.0.1", "0")
	if err != nil {
		t.Fatalf("Listener(127.0.0.1): %v", err)
	}
	l.Close()
}
