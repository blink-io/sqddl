package ddl

import (
	"testing"

	"github.com/blink-io/sq"
)

func TestSq_1(t *testing.T) {
	type MyTable struct {
		ID sq.NumberField `ddl:"id"`
	}
}
