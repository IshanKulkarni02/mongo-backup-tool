package config

import "testing"

func TestRedactURIMasksSecretQueryParameters(t *testing.T) {
	cases := map[string]string{
		"postgres://u:pw@h:5432/db?sslmode=disable&password=hunter2":         "postgres://u:****@h:5432/db?sslmode=disable&password=****",
		"mongodb://h/?tlsCertificateKeyFilePassword=s3cret&authSource=admin": "mongodb://h/?tlsCertificateKeyFilePassword=****&authSource=admin",
		"mongodb+srv://h/?retryWrites=true":                                  "mongodb+srv://h/?retryWrites=true",
		"postgres://h/db?token=abc%2Adef":                                    "postgres://h/db?token=abc%2Adef",
	}
	for in, want := range cases {
		if got := RedactURI(in); got != want {
			t.Errorf("RedactURI(%q)\n got  %q\n want %q", in, got, want)
		}
	}
}
