package delivery

import "testing"

func TestCheckoutDirNameSeparatesRepositoriesSharingAName(t *testing.T) {
	one := checkoutDirName("https://example.test/acme/web.git", "web")
	two := checkoutDirName("https://example.test/other/web.git", "web")
	if one == two {
		t.Fatalf("checkoutDirName collided for two different repositories: %q", one)
	}
	if RepositoryIdentity("git@example.test:acme/web.git") == RepositoryIdentity("https://example.test/acme/web.git") &&
		checkoutDirName("git@example.test:acme/web.git", "web") != one {
		t.Fatal("the same repository named two ways must clone to one directory")
	}
}
