package main

import (
	"bufio"
	"strings"
	"testing"
)

func TestReadSlice(t *testing.T) {
	s := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	r := bufio.NewReaderSize(strings.NewReader(s), 1)
	l, err := r.ReadSlice('n')
	t.Logf("%s, %v", string(l), err.Error())
}
