package kitebuilder

import (
	"bytes"
	"context"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/assetnote/kiterunner/pkg/proute"
	"github.com/stretchr/testify/assert"
)

func TestCompileOpenAPISpec(t *testing.T) {
	data, err := ioutil.ReadFile("../../pkg/openapi/testdata/petstore-v3.yaml")
	assert.NoError(t, err)

	output := filepath.Join(t.TempDir(), "petstore-v3")
	assert.NoError(t, Compile(context.Background(), bytes.NewReader(data), output))

	apis, err := proute.DecodeAPIProtoFile(output + ".kite")
	assert.NoError(t, err)
	assert.Len(t, apis, 1)
	assert.Len(t, apis[0].Routes, 11)
}
