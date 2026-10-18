package auth

import "testing"

func TestPasswordHashUsesArgon2idAndUniqueSalt(t *testing.T) {
	hasher := PasswordHasher{}
	first, err := hasher.Hash("long test password 1")
	if err != nil {
		t.Fatal(err)
	}
	second, err := hasher.Hash("long test password 1")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || first[:9] != "$argon2id" {
		t.Fatal("password hashes must use Argon2id with an individual salt")
	}
	if !hasher.Verify("long test password 1", first) || hasher.Verify("wrong password", first) {
		t.Fatal("password verification did not distinguish matching and non-matching passwords")
	}
	if hasher.Verify("x", "$argon2id$v=19$m=999999999,t=99,p=99$bad$bad") {
		t.Fatal("untrusted Argon2 parameters must be rejected")
	}
}
