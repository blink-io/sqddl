package ddl

import (
	"bytes"
	"fmt"
	"testing"
)

type EnumType string

func (e EnumType) Enumerate() []string {
	return []string{}
}

func TestBuf_1(t *testing.T) {
	buf := &bytes.Buffer{}
	buf.WriteString("Hello")
	buf.WriteString("World")
	buf.WriteString("!")
	fmt.Print(buf.String())
}
