package secret

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const password = "hunter2-do-not-log-me"

func TestRevealAndLen(t *testing.T) {
	v := FromString(password)
	if v.Reveal() != password {
		t.Fatalf("Reveal() = %q", v.Reveal())
	}
	if v.Len() != len(password) {
		t.Fatalf("Len() = %d, want %d", v.Len(), len(password))
	}
	if v.Empty() {
		t.Fatal("Empty() on a non-empty secret")
	}
	if string(v.Bytes()) != password {
		t.Fatalf("Bytes() = %q", v.Bytes())
	}
}

// The acceptance criterion: no credential value appears in anything rendered.
func TestSecretIsNeverRendered(t *testing.T) {
	v := FromString(password)

	rendered := []string{
		fmt.Sprintf("%v", v),
		fmt.Sprintf("%s", v),
		fmt.Sprintf("%q", v),
		fmt.Sprintf("%#v", v),
		fmt.Sprintf("%x", v),
		fmt.Sprintf("%+v", struct{ Password *Value }{v}),
		fmt.Errorf("auth failed: %w", fmt.Errorf("password %v rejected", v)).Error(),
		v.String(),
		v.GoString(),
	}

	for _, s := range rendered {
		if strings.Contains(s, password) {
			t.Fatalf("a rendered form leaked the secret: %q", s)
		}
		if !strings.Contains(s, redacted) {
			t.Fatalf("rendered form %q does not mark the secret as redacted", s)
		}
	}
}

func TestSecretIsNeverSerialised(t *testing.T) {
	v := FromString(password)

	if _, err := yaml.Marshal(v); !errors.Is(err, ErrNotSerialisable) {
		t.Fatalf("yaml.Marshal error = %v, want ErrNotSerialisable", err)
	}
	if _, err := json.Marshal(v); err == nil {
		t.Fatal("json.Marshal serialised a secret")
	} else if strings.Contains(err.Error(), password) {
		t.Fatalf("the marshal error leaked the secret: %v", err)
	}
	if _, err := v.MarshalText(); !errors.Is(err, ErrNotSerialisable) {
		t.Fatalf("MarshalText error = %v, want ErrNotSerialisable", err)
	}
}

func TestWipeClearsTheBuffer(t *testing.T) {
	buf := []byte(password)
	v := New(buf)
	v.Wipe()

	if v.Reveal() != "" || !v.Empty() {
		t.Fatalf("Reveal() after Wipe = %q", v.Reveal())
	}
	for i, b := range buf {
		if b != 0 {
			t.Fatalf("byte %d survived Wipe: %q", i, buf)
		}
	}
}

func TestNilValueIsUsable(t *testing.T) {
	var v *Value
	if !v.Empty() || v.Len() != 0 || v.Reveal() != "" || v.Bytes() != nil {
		t.Fatal("a nil Value did not behave as an empty secret")
	}
	v.Wipe() // must not panic
	if got := fmt.Sprintf("%v", v); got != redacted {
		t.Fatalf("nil Value rendered as %q", got)
	}
}
