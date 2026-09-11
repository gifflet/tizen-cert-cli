package profile

import "testing"

// Known ciphertexts produced by Tizen Studio's own CipherUtil, used as
// compatibility vectors.
func TestDecryptKnownVectors(t *testing.T) {
	cases := map[string]string{
		"Vy63flx5JBMc5GA4iEf8oFy+8aKE7FX/+arrDcO4I5k=": "tizenpkcs12passfordsigner",
		"xmEcrXPl1ss=": "",
	}
	for encrypted, want := range cases {
		got, err := DecryptPassword(encrypted)
		if err != nil {
			t.Fatalf("DecryptPassword(%q): %v", encrypted, err)
		}
		if got != want {
			t.Errorf("DecryptPassword(%q) = %q, want %q", encrypted, got, want)
		}
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	for _, plain := range []string{"MyP4ssword!", "", "tizenpkcs12passfordsigner", "áéç-unicode"} {
		encrypted, err := EncryptPassword(plain)
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecryptPassword(encrypted)
		if err != nil {
			t.Fatal(err)
		}
		if got != plain {
			t.Errorf("round trip of %q = %q", plain, got)
		}
	}
}

func TestEncryptMatchesKnownVector(t *testing.T) {
	encrypted, err := EncryptPassword("tizenpkcs12passfordsigner")
	if err != nil {
		t.Fatal(err)
	}
	if encrypted != "Vy63flx5JBMc5GA4iEf8oFy+8aKE7FX/+arrDcO4I5k=" {
		t.Errorf("EncryptPassword produced %q, does not match Tizen Studio's output", encrypted)
	}
}
