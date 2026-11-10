package wsl

import (
	"context"
	"encoding/binary"
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
)

func utf16le(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, len(u)*2)
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return b
}

func TestDecodeAndParse(t *testing.T) {
	raw := utf16le("  NAME            STATE           VERSION\r\n* Ubuntu-22.04    Running         2\r\n  docker-desktop  Stopped         2\r\n  Old Distro      Stopped         1\r\n")
	text := decode(raw)
	ds := parseList(text)
	assert.Len(t, ds, 3)
	assert.Equal(t, Distro{Name: "Ubuntu-22.04", State: "Running", Version: 2, Default: true}, ds[0])
	assert.Equal(t, "Old Distro", ds[2].Name)
	assert.Equal(t, 1, ds[2].Version)
}

func TestLocalisedHeaderStillParses(t *testing.T) {
	ds := parseList("  NOM   ÉTAT   VERSION\r\n* Debian  Running  2\r\n")
	assert.Len(t, ds, 1)
}

func TestGetStatusDoesNotPanic(t *testing.T) {
	s := GetStatus(context.Background())
	assert.NotEmpty(t, s.Message)
}
