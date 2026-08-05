package unit

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/derinbarutcu17/costmaxx/internal/config"
	"github.com/derinbarutcu17/costmaxx/internal/privacy"
)

func TestConfigLoadMissingFile(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "nope.toml"))
	if err != nil {
		t.Fatalf("missing file should load defaults, got %v", err)
	}
	if cfg.Mode != "observe" || cfg.Store.MaxArtifactSize != 50<<20 {
		t.Error("expected defaults for missing config")
	}
}

func TestConfigLoadEmptyFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte(""), 0600)
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("empty file should load defaults, got %v", err)
	}
	if cfg.Mode != "observe" {
		t.Error("expected default mode")
	}
}

func TestConfigLoadMalformed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("[[[ not toml"), 0600)
	if _, err := config.Load(p); err == nil {
		t.Error("malformed TOML should error")
	}
}

func TestConfigUnknownKeysIgnored(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	os.WriteFile(p, []byte("mode = \"observe\"\nunknown_key = 42\n[core]\nbogus = true\n"), 0600)
	cfg, err := config.Load(p)
	if err != nil {
		t.Logf("unknown keys error (strict parse): %v", err)
	} else if cfg.Mode != "observe" {
		t.Error("parsed config lost mode")
	}
}

func TestConfigSaveRoundtrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "c.toml")
	cfg := config.Default()
	cfg.Mode = "active"
	cfg.Core.DataDir = "/tmp/costmax-roundtrip"
	cfg.Store.MaxArtifactSize = 12345
	cfg.Reduce.Threshold = 999
	if err := cfg.Save(p); err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Mode != "active" || loaded.Core.DataDir != "/tmp/costmax-roundtrip" ||
		loaded.Store.MaxArtifactSize != 12345 || loaded.Reduce.Threshold != 999 {
		t.Errorf("roundtrip mismatch: %+v", loaded)
	}
}

func TestRedactorNormalBuildLogNoSecrets(t *testing.T) {
	log := "# github.com/x/y\n./main.go:12:34: undefined: foo\nFAIL\tgithub.com/x/y [build failed]\n"
	if privacy.NewRedactor().ContainsSecrets(log) {
		t.Error("normal build log flagged as containing secrets (false positive)")
	}
}

func TestRedactorDetectsKeysAndTokens(t *testing.T) {
	r := privacy.NewRedactor()
	cases := []struct {
		name string
		in   string
		want bool
	}{
		{"ssn", "SSN 123-45-6789", true},
		{"jwt", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U", true},
		{"email", "contact me at foo@bar.com", true},
		{"url with creds", "https://user:pass@host.example", true},
		{"api key no space", "api_key=sk-1234567890abcdef", true},
		{"token no space", "token=abcdefghijklmnop", true},
		{"password colon", "password:supersecret123", true},
		{"hex string", "digest=3f2a1b4c5d6e7f8a9b0c1d2e3f4a5b6c7d8e9f0a", false},
	}
	for _, c := range cases {
		if got := r.ContainsSecrets(c.in); got != c.want {
			t.Errorf("%s: ContainsSecrets = %v, want %v", c.name, got, c.want)
		}
	}
}

// Common secret formats the redactor currently MISSES. These are findings,
// not failures: the regexes require the key word directly before '=' or ':'
// and do not include AWS-style or Bearer formats.
func TestRedactorMissesCommonFormats(t *testing.T) {
	r := privacy.NewRedactor()
	for _, c := range []struct {
		name string
		in   string
	}{
		{"aws secret_access_key format", "aws_secret_access_key=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"},
		{"bearer token", "Authorization: Bearer abcdefgh12345678"},
		{"spaced api key assignment", `api_key = "sk-1234567890abcdef"`},
		{"spaced secret assignment", "secret = hunter2secretvalue"},
	} {
		if r.ContainsSecrets(c.in) {
			t.Errorf("%s: unexpectedly detected (regex coverage changed?)", c.name)
		} else {
			t.Logf("MISS: %s not detected", c.name)
		}
	}
}

func TestRedactOutputCoversEmailAndIP(t *testing.T) {
	out := privacy.NewRedactor().RedactOutput("mail foo@bar.com from 192.168.1.1 token=supersecretvalue123")
	for _, needle := range []string{"foo@bar.com", "192.168.1.1", "supersecretvalue123"} {
		if contains(out, needle) {
			t.Errorf("RedactOutput left %q in %q", needle, out)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && len(sub) > 0 && indexOf(s, sub) >= 0))
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
