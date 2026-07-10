package oci_test

import (
	"bytes"
	"context"
	"reflect"
	"testing"

	"github.com/lib-x/lzc-toolkit-go/oci"
)

func TestWriteAndReadLockRoundTrip(t *testing.T) {
	digest, err := oci.ParseDigest("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	want := oci.Lock{Version: 1, Images: map[string]oci.LockImage{
		"app": {ImageID: digest, Layers: []oci.LockLayer{{Digest: digest, Source: oci.LayerSourceEmbed}}},
	}}
	var output bytes.Buffer

	if err := oci.WriteLock(context.Background(), &output, want); err != nil {
		t.Fatal(err)
	}
	got, err := oci.ReadLock(context.Background(), bytes.NewReader(output.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
